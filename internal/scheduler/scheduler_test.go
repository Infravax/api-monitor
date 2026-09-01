package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/InfraVex/api-monitor/internal/checker"
	"github.com/InfraVex/api-monitor/internal/target"
)

// fakeLister is a TargetLister test double whose contents can be mutated
// mid-test (via set) to simulate target create/update/delete without
// wiring a real target.Service + repository.
type fakeLister struct {
	mu      sync.Mutex
	targets []target.Target
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newFakeLister(targets ...target.Target) *fakeLister {
	return &fakeLister{targets: targets}
}

func (f *fakeLister) List(_ context.Context) ([]target.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]target.Target, len(f.targets))
	copy(out, f.targets)
	return out, nil
}

func (f *fakeLister) set(targets []target.Target) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = targets
}

// fakeChecker is a TargetChecker test double that avoids real HTTP
// entirely: it records calls and returns an instant, canned result. This
// keeps scheduling-timing tests fast and deterministic, and separate from
// checker's own M3 tests, which cover real HTTP behavior.
type fakeChecker struct {
	mu    sync.Mutex
	calls []string // target IDs, in call order
	delay time.Duration
}

func (f *fakeChecker) Check(ctx context.Context, t target.Target) checker.CheckResult {
	f.mu.Lock()
	f.calls = append(f.calls, t.ID)
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
		}
	}

	r, err := checker.New(checker.NewParams{
		TargetID:   t.ID,
		Timestamp:  time.Now(),
		Outcome:    checker.OutcomeSuccess,
		StatusCode: 200,
	})
	if err != nil {
		panic(err)
	}
	return r
}

func (f *fakeChecker) count(targetID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.calls {
		if id == targetID {
			n++
		}
	}
	return n
}

func (f *fakeChecker) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// overlapDetectingChecker fails the test if it is ever re-entered for the
// same target ID while a previous call for that ID hasn't returned yet —
// a direct test of the "checks never overlap" policy, rather than an
// indirect inference from call counts.
type overlapDetectingChecker struct {
	t         *testing.T
	delay     time.Duration
	mu        sync.Mutex
	inFlight  map[string]bool
	callCount int
}

func newOverlapDetectingChecker(t *testing.T, delay time.Duration) *overlapDetectingChecker {
	return &overlapDetectingChecker{t: t, delay: delay, inFlight: make(map[string]bool)}
}

func (c *overlapDetectingChecker) Check(ctx context.Context, tg target.Target) checker.CheckResult {
	c.mu.Lock()
	if c.inFlight[tg.ID] {
		c.t.Errorf("overlapping check detected for target %s", tg.ID)
	}
	c.inFlight[tg.ID] = true
	c.callCount++
	c.mu.Unlock()

	select {
	case <-time.After(c.delay):
	case <-ctx.Done():
	}

	c.mu.Lock()
	c.inFlight[tg.ID] = false
	c.mu.Unlock()

	r, err := checker.New(checker.NewParams{
		TargetID: tg.ID, Timestamp: time.Now(), Outcome: checker.OutcomeSuccess, StatusCode: 200,
	})
	if err != nil {
		panic(err)
	}
	return r
}

func newTestTarget(t *testing.T, name string, interval time.Duration, enabled bool) target.Target {
	t.Helper()
	tg, err := target.New(target.NewParams{
		Name:               name,
		URL:                "https://example.com/" + name,
		Method:             "GET",
		Interval:           interval,
		Timeout:            time.Second,
		ExpectedStatusCode: 200,
	})
	if err != nil {
		t.Fatalf("target.New() unexpected error: %v", err)
	}
	tg.Enabled = enabled
	return tg
}

func TestScheduler_SingleTarget(t *testing.T) {
	tg := newTestTarget(t, "a", 30*time.Millisecond, true)
	fc := &fakeChecker{}
	s := New(Config{Targets: newFakeLister(tg), Checker: fc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 160*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	// Immediate check + ticks over ~160ms at 30ms each ~= 5-6 calls;
	// assert loosely to avoid flakiness under scheduling jitter.
	if n := fc.count(tg.ID); n < 3 {
		t.Errorf("Check called %d times, want at least 3", n)
	}
}

func TestScheduler_MultipleTargets_RespectOwnIntervals(t *testing.T) {
	fast := newTestTarget(t, "fast", 40*time.Millisecond, true)
	slow := newTestTarget(t, "slow", 160*time.Millisecond, true)
	fc := &fakeChecker{}
	s := New(Config{Targets: newFakeLister(fast, slow), Checker: fc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	fastCount := fc.count(fast.ID)
	slowCount := fc.count(slow.ID)

	if fastCount <= slowCount {
		t.Errorf("fast target called %d times, slow target called %d times; want fast > slow", fastCount, slowCount)
	}
	if slowCount < 1 {
		t.Errorf("slow target was never called")
	}
}

func TestScheduler_DisabledTarget(t *testing.T) {
	enabled := newTestTarget(t, "enabled", 30*time.Millisecond, true)
	disabled := newTestTarget(t, "disabled", 30*time.Millisecond, false)
	fc := &fakeChecker{}
	s := New(Config{Targets: newFakeLister(enabled, disabled), Checker: fc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	if n := fc.count(disabled.ID); n != 0 {
		t.Errorf("disabled target was checked %d times, want 0", n)
	}
	if n := fc.count(enabled.ID); n < 1 {
		t.Errorf("enabled target was never checked")
	}
}

func TestScheduler_Cancellation(t *testing.T) {
	tg := newTestTarget(t, "a", 15*time.Millisecond, true)
	fc := &fakeChecker{}
	s := New(Config{Targets: newFakeLister(tg), Checker: fc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx)
		close(done)
	}()

	time.Sleep(80 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Start() did not return promptly after context cancellation")
	}

	countAtShutdown := fc.total()
	time.Sleep(100 * time.Millisecond)
	if n := fc.total(); n != countAtShutdown {
		t.Errorf("checks continued after shutdown: %d calls at shutdown, %d calls 100ms later", countAtShutdown, n)
	}
}

func TestScheduler_LongRunningCheck_NoOverlap(t *testing.T) {
	// Interval (20ms) is much shorter than how long each check takes
	// (150ms) — this is exactly the "interval < check duration" scenario
	// the skip-if-still-running policy exists for.
	tg := newTestTarget(t, "slow-check", 20*time.Millisecond, true)
	oc := newOverlapDetectingChecker(t, 150*time.Millisecond)
	s := New(Config{Targets: newFakeLister(tg), Checker: oc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	// t.Errorf inside oc.Check already fails the test on any overlap. As
	// a sanity check, the call count should be roughly total/delay
	// (~2-3), nowhere near total/interval (~20), which is what unbounded
	// overlapping would have produced.
	if oc.callCount > 5 {
		t.Errorf("callCount = %d, want a small number consistent with non-overlapping execution", oc.callCount)
	}
	if oc.callCount < 1 {
		t.Error("Check was never called")
	}
}

func TestScheduler_TargetUpdate(t *testing.T) {
	// Starts with a long interval so it won't naturally re-tick during
	// the "before" window, then the test shortens it to prove the update
	// takes effect once the scheduler notices (DiscoveryInterval).
	tg := newTestTarget(t, "a", 500*time.Millisecond, true)
	lister := newFakeLister(tg)
	fc := &fakeChecker{}
	s := New(Config{Targets: lister, Checker: fc, DiscoveryInterval: 30 * time.Millisecond, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	countBefore := fc.count(tg.ID)

	updated := tg
	updated.Interval = 20 * time.Millisecond
	lister.set([]target.Target{updated})

	<-done

	countAfter := fc.count(tg.ID)
	if countAfter-countBefore < 3 {
		t.Errorf("after shortening the interval, only %d additional checks ran (before=%d, after=%d); want several more",
			countAfter-countBefore, countBefore, countAfter)
	}
}

func TestScheduler_TargetDeletion(t *testing.T) {
	tg := newTestTarget(t, "a", 20*time.Millisecond, true)
	lister := newFakeLister(tg)
	fc := &fakeChecker{}
	s := New(Config{Targets: lister, Checker: fc, DiscoveryInterval: 30 * time.Millisecond, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Start(ctx)
		close(done)
	}()

	time.Sleep(60 * time.Millisecond)
	if n := fc.count(tg.ID); n < 1 {
		t.Fatal("target was never checked before deletion")
	}

	lister.set(nil) // simulate deletion

	// Give the scheduler a couple of discovery cycles to notice, then
	// measure whether checks have actually stopped.
	time.Sleep(80 * time.Millisecond)
	countAfterDeletion := fc.count(tg.ID)
	time.Sleep(80 * time.Millisecond)

	<-done

	if final := fc.count(tg.ID); final != countAfterDeletion {
		t.Errorf("checks continued after deletion: %d calls shortly after delete, %d calls later", countAfterDeletion, final)
	}
}

func TestScheduler_ConcurrentTargets(t *testing.T) {
	const n = 15
	targets := make([]target.Target, n)
	for i := range targets {
		targets[i] = newTestTarget(t, string(rune('a'+i)), 20*time.Millisecond, true)
	}

	fc := &fakeChecker{}
	s := New(Config{Targets: newFakeLister(targets...), Checker: fc, DiscoveryInterval: time.Hour, Logger: discardLogger()})

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_ = s.Start(ctx)

	for _, tg := range targets {
		if fc.count(tg.ID) < 1 {
			t.Errorf("target %s was never checked", tg.Name)
		}
	}
}
