package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/InfraVex/api-monitor/internal/checker"
	"github.com/InfraVex/api-monitor/internal/target"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestTarget(t *testing.T, name string) target.Target {
	t.Helper()
	tg, err := target.New(target.NewParams{
		Name: name, URL: "https://example.com/" + name, Method: "GET",
		Interval: time.Minute, Timeout: time.Second, ExpectedStatusCode: 200,
	})
	if err != nil {
		t.Fatalf("target.New() unexpected error: %v", err)
	}
	return tg
}

func successResult(t *testing.T, targetID string) checker.CheckResult {
	t.Helper()
	r, err := checker.New(checker.NewParams{
		TargetID: targetID, Timestamp: time.Now(), Outcome: checker.OutcomeSuccess, StatusCode: 200,
	})
	if err != nil {
		t.Fatalf("checker.New() unexpected error: %v", err)
	}
	return r
}

// fakeChecker is a Checker test double that avoids real HTTP entirely,
// tracks concurrency, and can be configured to delay, block on a gate, or
// panic — everything the M5 concurrency tests need without any real
// network dependency.
type fakeChecker struct {
	delay time.Duration
	gate  chan struct{} // if non-nil, Check blocks until this is closed
	panic bool

	mu          sync.Mutex
	current     int
	maxObserved int
	calls       []string
}

func (f *fakeChecker) Check(ctx context.Context, t target.Target) checker.CheckResult {
	f.mu.Lock()
	f.current++
	if f.current > f.maxObserved {
		f.maxObserved = f.current
	}
	f.calls = append(f.calls, t.ID)
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.current--
		f.mu.Unlock()
	}()

	if f.panic {
		panic("simulated check panic")
	}

	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
		}
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
		}
	}

	return successResultUnsafe(t.ID)
}

func successResultUnsafe(targetID string) checker.CheckResult {
	r, err := checker.New(checker.NewParams{
		TargetID: targetID, Timestamp: time.Now(), Outcome: checker.OutcomeSuccess, StatusCode: 200,
	})
	if err != nil {
		panic(err)
	}
	return r
}

func (f *fakeChecker) maxConcurrency() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxObserved
}

func (f *fakeChecker) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func TestPool_ExecutesJobs(t *testing.T) {
	fc := &fakeChecker{}
	p := New(Config{Checker: fc, WorkerCount: 3, QueueSize: 10, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	tg := newTestTarget(t, "a")
	result := p.Check(context.Background(), tg)

	if result.Outcome != checker.OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, checker.OutcomeSuccess)
	}
	if result.TargetID != tg.ID {
		t.Errorf("TargetID = %q, want %q", result.TargetID, tg.ID)
	}

	cancel()
	<-done
}

// TestPool_WorkerLimit is the most important M5 test: it proves that no
// more than WorkerCount checks ever run concurrently, even when far more
// than WorkerCount targets become due at once.
func TestPool_WorkerLimit(t *testing.T) {
	const workerCount = 5
	const targetCount = 20

	fc := &fakeChecker{delay: 80 * time.Millisecond}
	p := New(Config{Checker: fc, WorkerCount: workerCount, QueueSize: targetCount, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	targets := make([]target.Target, targetCount)
	for i := range targets {
		targets[i] = newTestTarget(t, string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	for _, tg := range targets {
		wg.Add(1)
		go func(tg target.Target) {
			defer wg.Done()
			p.Check(context.Background(), tg)
		}(tg)
	}
	wg.Wait()

	if got := fc.maxConcurrency(); got != workerCount {
		t.Errorf("max observed concurrency = %d, want exactly %d", got, workerCount)
	}

	cancel()
	<-done
}

func TestPool_QueueCapacity_BlocksThenUnblocks(t *testing.T) {
	fc := &fakeChecker{gate: make(chan struct{})}
	// 1 worker, queue size 1: worker picks up job1 (gate closed keeps it
	// running), job2 fills the only queue slot, job3 has nowhere to go
	// and must block in Submit until capacity frees up.
	p := New(Config{Checker: fc, WorkerCount: 1, QueueSize: 1, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	tg1, tg2, tg3 := newTestTarget(t, "1"), newTestTarget(t, "2"), newTestTarget(t, "3")

	result1 := make(chan checker.CheckResult, 1)
	go func() { result1 <- p.Check(context.Background(), tg1) }()
	// Give the worker time to actually pick up job1 (so it's "running",
	// not just "queued") before job2/job3 are submitted.
	time.Sleep(30 * time.Millisecond)

	result2 := make(chan checker.CheckResult, 1)
	go func() { result2 <- p.Check(context.Background(), tg2) }()
	time.Sleep(30 * time.Millisecond) // let job2 land in the queue's 1 slot

	result3Done := make(chan checker.CheckResult, 1)
	go func() { result3Done <- p.Check(context.Background(), tg3) }()

	select {
	case <-result3Done:
		t.Fatal("Check(tg3) returned before capacity was available; it should have blocked")
	case <-time.After(80 * time.Millisecond):
		// expected: still blocked
	}

	close(fc.gate) // release job1; job2 then job3 can proceed in turn

	for i, ch := range []chan checker.CheckResult{result1, result2, result3Done} {
		select {
		case r := <-ch:
			if r.Outcome != checker.OutcomeSuccess {
				t.Errorf("result %d Outcome = %q, want success", i+1, r.Outcome)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("result %d never arrived after releasing the gate", i+1)
		}
	}

	cancel()
	<-done
}

func TestPool_Shutdown(t *testing.T) {
	fc := &fakeChecker{}
	p := New(Config{Checker: fc, WorkerCount: 3, QueueSize: 10, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = p.Start(ctx)
		close(done)
	}()

	// Let workers actually start before cancelling.
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return promptly after context cancellation")
	}
}

func TestPool_CancellationDuringWait(t *testing.T) {
	// No worker running at all: Submit must still return promptly once
	// ctx is canceled, rather than blocking forever waiting for a result
	// that will never come.
	fc := &fakeChecker{}
	p := New(Config{Checker: fc, WorkerCount: 1, QueueSize: 0, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	tg := newTestTarget(t, "a")

	start := time.Now()
	result := p.Check(ctx, tg)
	elapsed := time.Since(start)

	if result.Outcome != checker.OutcomeCanceled {
		t.Errorf("Outcome = %q, want %q", result.Outcome, checker.OutcomeCanceled)
	}
	if elapsed > time.Second {
		t.Errorf("Check took %v, want prompt return near the 50ms deadline", elapsed)
	}
}

func TestPool_CheckFailureDoesNotStopWorker(t *testing.T) {
	fc := &failThenSucceedChecker{}
	p := New(Config{Checker: fc, WorkerCount: 1, QueueSize: 5, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	tg1 := newTestTarget(t, "fails")
	r1 := p.Check(context.Background(), tg1)
	if r1.Outcome != checker.OutcomeConnectionError {
		t.Fatalf("first result Outcome = %q, want %q", r1.Outcome, checker.OutcomeConnectionError)
	}

	tg2 := newTestTarget(t, "succeeds")
	r2 := p.Check(context.Background(), tg2)
	if r2.Outcome != checker.OutcomeSuccess {
		t.Fatalf("second result Outcome = %q, want %q (worker should keep processing after a failed check)", r2.Outcome, checker.OutcomeSuccess)
	}

	cancel()
	<-done
}

// failThenSucceedChecker returns a normal (non-panicking) failure result
// for the first call, then succeeds — used to prove a failed check is
// just data, not something that disrupts the worker.
type failThenSucceedChecker struct {
	mu    sync.Mutex
	calls int
}

func (f *failThenSucceedChecker) Check(_ context.Context, t target.Target) checker.CheckResult {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()

	if n == 1 {
		r, err := checker.New(checker.NewParams{
			TargetID: t.ID, Timestamp: time.Now(), Outcome: checker.OutcomeConnectionError, ErrorMessage: "connection refused",
		})
		if err != nil {
			panic(err)
		}
		return r
	}
	return successResultUnsafe(t.ID)
}

func TestPool_PanicRecovery(t *testing.T) {
	fc := &fakeChecker{panic: true}
	p := New(Config{Checker: fc, WorkerCount: 1, QueueSize: 5, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	tg := newTestTarget(t, "panics")

	// Must not crash the test process, and must still return a result.
	result := p.Check(context.Background(), tg)

	if result.Outcome != checker.OutcomeConnectionError {
		t.Errorf("Outcome = %q, want %q", result.Outcome, checker.OutcomeConnectionError)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage is empty, want a description mentioning the panic")
	}

	// The worker goroutine must have survived and still be able to
	// process a subsequent, normal job.
	fc2 := &fakeChecker{}
	p.checker = fc2 // swap in a non-panicking checker for the next job
	tg2 := newTestTarget(t, "recovers")
	result2 := p.Check(context.Background(), tg2)
	if result2.Outcome != checker.OutcomeSuccess {
		t.Errorf("after panic recovery, subsequent check Outcome = %q, want %q", result2.Outcome, checker.OutcomeSuccess)
	}

	cancel()
	<-done
}

func TestPool_MultipleTargetsConcurrently(t *testing.T) {
	const targetCount = 10
	fc := &fakeChecker{delay: 20 * time.Millisecond}
	p := New(Config{Checker: fc, WorkerCount: targetCount, QueueSize: targetCount, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	targets := make([]target.Target, targetCount)
	for i := range targets {
		targets[i] = newTestTarget(t, string(rune('a'+i)))
	}

	start := time.Now()
	var wg sync.WaitGroup
	for _, tg := range targets {
		wg.Add(1)
		go func(tg target.Target) {
			defer wg.Done()
			r := p.Check(context.Background(), tg)
			if r.Outcome != checker.OutcomeSuccess {
				t.Errorf("target %s Outcome = %q, want success", tg.Name, r.Outcome)
			}
		}(tg)
	}
	wg.Wait()
	elapsed := time.Since(start)

	// With enough workers for every target, all 10 checks (20ms each)
	// should overlap rather than run one after another (~200ms serial).
	if elapsed > 150*time.Millisecond {
		t.Errorf("elapsed = %v, want well under serial execution time (~200ms), indicating checks ran concurrently", elapsed)
	}

	cancel()
	<-done
}

func TestPool_New_PanicsWithoutChecker(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic with a nil Checker")
		}
	}()
	New(Config{})
}

func TestPool_Submit_ReturnsContextError(t *testing.T) {
	fc := &fakeChecker{gate: make(chan struct{})} // never released within the test
	p := New(Config{Checker: fc, WorkerCount: 1, QueueSize: 0, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	// Occupy the single worker so it can never help submit/serve a second job.
	tg1 := newTestTarget(t, "busy")
	go p.Check(context.Background(), tg1)
	time.Sleep(20 * time.Millisecond)

	submitCtx, submitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer submitCancel()

	tg2 := newTestTarget(t, "blocked")
	_, err := p.Submit(submitCtx, tg2)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Submit() error = %v, want %v", err, context.DeadlineExceeded)
	}

	close(fc.gate)
	cancel()
	<-done
}

func BenchmarkPool_Submit(b *testing.B) {
	fc := &fakeChecker{}
	p := New(Config{Checker: fc, WorkerCount: 20, QueueSize: 1000, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = p.Start(ctx); close(done) }()

	tg, err := target.New(target.NewParams{
		Name: "bench", URL: "https://example.com", Method: "GET",
		Interval: time.Minute, Timeout: time.Second, ExpectedStatusCode: 200,
	})
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	var counter atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			p.Check(context.Background(), tg)
			counter.Add(1)
		}
	})
	b.StopTimer()

	cancel()
	<-done
}
