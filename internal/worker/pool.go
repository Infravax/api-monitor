package worker

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/InfraVex/api-monitor/internal/checker"
	"github.com/InfraVex/api-monitor/internal/target"
)

const (
	defaultWorkerCount = 10
	defaultQueueSize   = 100
)

// Checker is the subset of *checker.Checker the Pool needs: perform one
// check and return its result. Defined here, by the consumer, for the
// same reason internal/scheduler defines its own TargetChecker: it lets
// Pool's tests inject a fast, controllable stub instead of every test
// needing a real HTTP round-trip.
type Checker interface {
	Check(ctx context.Context, t target.Target) checker.CheckResult
}

// job is one pending unit of work.
//
// result is buffered with capacity 1 so a worker can always deliver a
// result without blocking, even if Submit's caller has already given up
// waiting due to ctx cancellation. Without the buffer, that worker
// goroutine would block forever trying to send to a receiver that will
// never come back — exactly the kind of goroutine leak this package
// exists to prevent elsewhere.
type job struct {
	target     target.Target
	enqueuedAt time.Time // for queue-latency logging, not scheduling logic
	result     chan checker.CheckResult
}

// Config holds the Pool's dependencies and tunables.
type Config struct {
	// Checker performs one check against a target. Required.
	Checker Checker
	// WorkerCount is how many checks may run concurrently. This is an
	// I/O-bound workload (waiting on network round-trips to third-party
	// APIs), not CPU-bound, so it is not derived from runtime.NumCPU() —
	// a low core count shouldn't artificially cap how many concurrent
	// HTTP requests can be in flight. Defaults to defaultWorkerCount (10)
	// if <= 0: deliberately conservative, since these requests target
	// third-party APIs InfraVex doesn't own, and a new deployment
	// shouldn't hammer them by default. Operators with larger fleets can
	// raise it explicitly (WORKER_COUNT).
	WorkerCount int
	// QueueSize is how many submitted jobs may wait for a free worker
	// before Submit starts blocking its caller. Defaults to
	// defaultQueueSize (100) if <= 0: enough to absorb a burst (e.g. many
	// targets becoming due close together, or every target's immediate
	// first check right after a scheduler restart) without immediately
	// blocking, while staying small — each job is a small value, but an
	// unbounded queue would just move the "10,000 unbounded goroutines"
	// memory problem into "10,000 unbounded queued jobs" instead of
	// actually solving it.
	QueueSize int
	// Logger receives pool lifecycle events. Defaults to slog.Default()
	// if nil.
	Logger *slog.Logger
}

// Pool runs a fixed number of worker goroutines that pull jobs from a
// bounded queue and execute them via Checker. It holds only immutable
// configuration plus the queue channel itself; all other state is local
// to a Start call — the same "no shared mutable state beyond config"
// shape as scheduler.Scheduler.
type Pool struct {
	checker     Checker
	logger      *slog.Logger
	workerCount int
	queue       chan job
}

// New creates a Pool from cfg. It panics if Config.Checker is missing —
// this is application wiring code invoked once at startup from main.go,
// not driven by external input, so failing fast with a clear message
// beats a later, more confusing nil-pointer panic inside a worker
// goroutine.
func New(cfg Config) *Pool {
	if cfg.Checker == nil {
		panic("worker: Config.Checker is required")
	}

	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = defaultWorkerCount
	}
	queueSize := cfg.QueueSize
	if queueSize <= 0 {
		queueSize = defaultQueueSize
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Pool{
		checker:     cfg.Checker,
		logger:      logger,
		workerCount: workerCount,
		queue:       make(chan job, queueSize),
	}
}

// Start runs Pool's worker goroutines until ctx is canceled, then returns
// once every one of them has actually exited — no goroutines are left
// running after Start returns. Same ctx-driven lifecycle model as
// scheduler.Scheduler.Start: no separate Stop method.
//
// Shutdown policy: a worker that has already picked up a job keeps
// running it to completion — which resolves quickly on its own once ctx
// is canceled, because checker.Checker (M3) aborts an in-flight HTTP
// request promptly via the same ctx. Workers do not drain the rest of the
// queue on shutdown: once ctx is done, a worker simply stops pulling new
// jobs, rather than working through everything still queued. Any jobs
// still sitting in the queue at that point are abandoned. This is
// deliberate: draining a potentially-deep queue on shutdown could make
// shutdown take as long as the queue is deep, risking the same "must not
// hang" failure this project has avoided since M2's graceful shutdown —
// and a check dropped this way is simply attempted again on the target's
// own next scheduled interval after restart, the same trade-off M4
// already accepted for a check still running when the process stops.
func (p *Pool) Start(ctx context.Context) error {
	p.logger.Info("worker pool started", "worker_count", p.workerCount, "queue_size", cap(p.queue))
	defer p.logger.Info("worker pool stopped")

	var wg sync.WaitGroup
	for i := 0; i < p.workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p.runWorker(ctx, id)
		}(i)
	}

	wg.Wait()
	return nil
}

func (p *Pool) runWorker(ctx context.Context, id int) {
	p.logger.Info("worker started", "worker_id", id)
	defer p.logger.Info("worker stopped", "worker_id", id)

	for {
		select {
		case <-ctx.Done():
			return
		case j := <-p.queue:
			p.process(ctx, j)
		}
	}
}

// process runs one job and delivers its result. queue_wait in the log
// line is an approximation (total time since enqueue minus the check's
// own latency), not an exact measurement, which is precise enough for
// operational visibility without adding a dedicated timer.
func (p *Pool) process(ctx context.Context, j job) {
	result := p.safeCheck(ctx, j.target)

	p.logger.Info("check completed",
		"target_id", j.target.ID,
		"target_name", j.target.Name,
		"outcome", result.Outcome,
		"status_code", result.StatusCode,
		"latency", result.Latency,
		"queue_wait", time.Since(j.enqueuedAt)-result.Latency,
	)

	j.result <- result
}

// safeCheck recovers a panic from Checker.Check so that one bad check —
// a bug, never a legitimate monitoring outcome — cannot take down this
// worker, let alone the whole pool or process. A recovered panic is
// logged loudly at Error with the panic value and a stack trace: it is
// never hidden as a normal check failure. The waiting Submit call still
// needs a result to receive, so the recovered panic is reported as
// OutcomeConnectionError — reusing an existing Outcome rather than adding
// a new domain concept for what is meant to be an unreachable safety net,
// not an expected code path.
func (p *Pool) safeCheck(ctx context.Context, t target.Target) (result checker.CheckResult) {
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("worker: recovered from panic during check",
				"target_id", t.ID, "panic", r, "stack", string(debug.Stack()))
			result, _ = checker.New(checker.NewParams{
				TargetID:     t.ID,
				Timestamp:    time.Now(),
				Outcome:      checker.OutcomeConnectionError,
				ErrorMessage: fmt.Sprintf("worker: recovered from panic: %v", r),
			})
		}
	}()
	return p.checker.Check(ctx, t)
}

// Check submits t and blocks until a worker has produced a result, or ctx
// is canceled first. It has the same signature as
// scheduler.TargetChecker/checker.Checker.Check, so a *Pool is a drop-in
// replacement wherever a *checker.Checker was used directly — in
// particular, internal/scheduler needed no code changes at all for M5;
// only main.go's wiring changed what it hands the Scheduler as its
// Checker.
func (p *Pool) Check(ctx context.Context, t target.Target) checker.CheckResult {
	result, err := p.Submit(ctx, t)
	if err == nil {
		return result
	}

	// Submission itself was aborted — ctx was canceled before a worker
	// could even start on it — so this is not a target-side observation.
	// It's represented the same way M3 represents any other
	// caller-initiated abort: OutcomeCanceled.
	r, buildErr := checker.New(checker.NewParams{
		TargetID:     t.ID,
		Timestamp:    time.Now(),
		Outcome:      checker.OutcomeCanceled,
		ErrorMessage: err.Error(),
	})
	if buildErr != nil {
		panic(fmt.Sprintf("worker: internal invariant violation building CheckResult: %v", buildErr))
	}
	return r
}

// Submit enqueues a check for t and waits for its result. It returns a
// non-nil error only if ctx was canceled before the job could be
// submitted or before a result arrived — never as a way to report a
// failed *check* (that's carried inside the returned CheckResult's
// Outcome, matching checker.Checker.Check's own convention).
//
// Submit blocks its caller when the queue is full, rather than dropping
// the job or replacing a pending one. This is the explicit backpressure
// policy for M5: no scheduled check is silently lost when the system is
// busy, it simply waits its turn. Blocking here is safe specifically
// because scheduler.Scheduler only ever calls Check for one target at a
// time per target (see its own "checks never overlap" invariant from
// M4) — so the number of goroutines that could ever be blocked in Submit
// at once is bounded by the number of targets, not unbounded. Dropping
// was rejected because it creates gaps in monitoring history exactly
// when the system is under load, which is when observability matters
// most. A future event-driven pipeline (M9) may revisit this once
// there's a durable queue (e.g. Kafka) able to absorb bursts without an
// in-process blocking caller at all.
func (p *Pool) Submit(ctx context.Context, t target.Target) (checker.CheckResult, error) {
	j := job{target: t, enqueuedAt: time.Now(), result: make(chan checker.CheckResult, 1)}

	select {
	case p.queue <- j:
	case <-ctx.Done():
		return checker.CheckResult{}, ctx.Err()
	default:
		// Queue is full right now. Log once, then actually block — this
		// keeps the common case silent while making sustained saturation
		// visible without polling. Under prolonged, heavy saturation this
		// could still log frequently; a queue_depth/queue_full metric
		// (see docs/architecture.md's future-metrics list) is the better
		// long-term answer, deliberately deferred rather than adding
		// ad-hoc rate-limiting here.
		p.logger.Warn("worker: queue full, waiting for capacity", "target_id", t.ID)
		select {
		case p.queue <- j:
		case <-ctx.Done():
			return checker.CheckResult{}, ctx.Err()
		}
	}

	select {
	case result := <-j.result:
		return result, nil
	case <-ctx.Done():
		return checker.CheckResult{}, ctx.Err()
	}
}
