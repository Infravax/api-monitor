package scheduler

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/InfraVex/api-monitor/internal/checker"
	"github.com/InfraVex/api-monitor/internal/target"
)

// defaultDiscoveryInterval is used when Config.DiscoveryInterval is left
// unset (<= 0).
const defaultDiscoveryInterval = 10 * time.Second

// TargetLister is the subset of *target.Service the Scheduler needs: just
// enough to discover the current set of targets. It is defined here, by
// the consumer, rather than the Scheduler depending on the full
// *target.Service — the same pattern target.Repository already
// established in M2 (interface owned by the package that uses it, not the
// package that implements it). *target.Service already satisfies this
// with no changes. The benefit here is concrete: scheduler tests can
// inject a lightweight fake instead of wiring a real service + repository.
type TargetLister interface {
	List(ctx context.Context) ([]target.Target, error)
}

// TargetChecker is the subset of *checker.Checker the Scheduler needs:
// perform one check and return its result. Also consumer-defined, for the
// same reason as TargetLister. This does not reverse checker.Checker's own
// M3 decision to stay a concrete type with no interface of its own — that
// was about checker's package having only one implementation and no
// caller needing to swap it. This is a different, newer decision by a
// different consumer (Scheduler) that needs to substitute a fast, in-test
// stub so scheduling-timing tests don't depend on real HTTP round-trips.
// *checker.Checker already satisfies this with no changes.
type TargetChecker interface {
	Check(ctx context.Context, t target.Target) checker.CheckResult
}

// Config holds the Scheduler's dependencies and tunables. A struct is used
// for the same reason as elsewhere in this codebase (target.NewParams,
// api.ServerConfig): named fields avoid ambiguity at the call site.
type Config struct {
	// Targets provides the current set of targets on demand. Required.
	Targets TargetLister
	// Checker performs one check against a target. Required.
	Checker TargetChecker
	// Logger receives scheduler lifecycle events. Defaults to
	// slog.Default() if nil.
	Logger *slog.Logger
	// DiscoveryInterval is how often the Scheduler re-lists targets to
	// notice new, updated, disabled, or deleted ones. This is a
	// scheduler-level operational setting, distinct from any single
	// target's own check Interval. It bounds how stale scheduling can be
	// relative to the target service's actual state — up to
	// DiscoveryInterval may pass before a create/update/delete made
	// through the REST API is reflected in what gets checked. Defaults to
	// defaultDiscoveryInterval if <= 0.
	DiscoveryInterval time.Duration
	// OnResult, if set, is called with each CheckResult as it is
	// produced. It is the only mechanism M4 has for a result to leave the
	// Scheduler: there is no persistence yet (M6) and no incident
	// interpretation yet (M7), so the smallest sensible mechanism for now
	// is a plain callback. It is called synchronously, in the same
	// per-target goroutine that just ran the check — a slow OnResult
	// delays that target's own next tick. That is an acceptable
	// limitation today because the only current callers log a line, but
	// it is exactly the seam M5's worker pool is expected to replace with
	// something that decouples check execution from result consumption.
	OnResult func(checker.CheckResult)
}

// Scheduler triggers periodic checks for enabled targets. It holds only
// immutable configuration; all per-run state lives on the stack of a
// Start call, so a Scheduler has no shared mutable state of its own and
// Start can be called more than once (sequentially) on the same instance.
type Scheduler struct {
	targets           TargetLister
	checker           TargetChecker
	logger            *slog.Logger
	discoveryInterval time.Duration
	onResult          func(checker.CheckResult)
}

// New creates a Scheduler from cfg. It panics if a required dependency
// (Targets or Checker) is missing — this is application wiring code
// invoked once at startup from main.go, not something driven by external
// input, so failing fast with a clear message beats a much later, more
// confusing nil-pointer panic during the first reconciliation pass.
func New(cfg Config) *Scheduler {
	if cfg.Targets == nil {
		panic("scheduler: Config.Targets is required")
	}
	if cfg.Checker == nil {
		panic("scheduler: Config.Checker is required")
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	discoveryInterval := cfg.DiscoveryInterval
	if discoveryInterval <= 0 {
		discoveryInterval = defaultDiscoveryInterval
	}

	return &Scheduler{
		targets:           cfg.Targets,
		checker:           cfg.Checker,
		logger:            logger,
		discoveryInterval: discoveryInterval,
		onResult:          cfg.OnResult,
	}
}

// runningTarget tracks one target's active per-target goroutine, so a
// later reconciliation pass can detect whether that target's
// configuration has changed since the goroutine was started.
type runningTarget struct {
	cancel context.CancelFunc
	target target.Target
}

// Start runs the Scheduler until ctx is canceled, then returns nil once
// every per-target goroutine it started has actually exited — no
// goroutines are left running after Start returns. There is no separate
// Stop method: shutdown is entirely ctx-driven, the same lifecycle model
// used by the M2 HTTP server (both are started from main.go against the
// same top-level context from signal.NotifyContext, so one SIGTERM stops
// both).
//
// Start's only job is reconciliation: periodically (every
// DiscoveryInterval) list the current targets and compare them against
// what is currently scheduled, starting, restarting, or stopping
// per-target goroutines as needed. This is what makes target create,
// update, enable/disable, and delete all "just work" without bespoke
// handling for each case (see reconcile) — at the cost of up to
// DiscoveryInterval of staleness before a change takes effect, which is
// an explicit, documented trade-off rather than an oversight: a push/event
// mechanism from target.Service to Scheduler would remove that staleness
// window, but nothing in M4 justifies that complexity yet.
func (s *Scheduler) Start(ctx context.Context) error {
	s.logger.Info("scheduler started", "discovery_interval", s.discoveryInterval)
	defer s.logger.Info("scheduler stopped")

	running := make(map[string]runningTarget)
	var wg sync.WaitGroup
	defer func() {
		for _, rt := range running {
			rt.cancel()
		}
		wg.Wait()
	}()

	// Reconcile once immediately, so the first batch of targets doesn't
	// wait a full DiscoveryInterval before being scheduled at all.
	s.reconcile(ctx, &wg, running)

	ticker := time.NewTicker(s.discoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.reconcile(ctx, &wg, running)
		}
	}
}

// reconcile lists the current targets and brings the set of running
// per-target goroutines in line with them:
//   - a new enabled target starts a goroutine for it
//   - a target whose configuration changed (any field — not just Interval,
//     since the running goroutine closes over a target.Target value that
//     would otherwise go stale) is stopped and restarted with the new value
//   - a disabled target is stopped
//   - a target no longer present at all (deleted) is stopped
//
// target.Target is a plain comparable struct (no pointers/slices), so
// detecting "did this target change" is a direct == comparison against the
// value the running goroutine was last started with.
func (s *Scheduler) reconcile(ctx context.Context, wg *sync.WaitGroup, running map[string]runningTarget) {
	targets, err := s.targets.List(ctx)
	if err != nil {
		s.logger.Error("scheduler: failed to list targets, keeping previous schedule", "error", err)
		return
	}

	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		seen[t.ID] = true

		if !t.Enabled {
			s.stop(running, t.ID, "target disabled")
			continue
		}

		if rt, ok := running[t.ID]; ok {
			if rt.target == t {
				continue // already running with the current configuration
			}
			s.stop(running, t.ID, "target configuration changed")
		}

		s.start(ctx, wg, running, t)
	}

	for id := range running {
		if !seen[id] {
			s.stop(running, id, "target deleted")
		}
	}
}

// start spawns the per-target goroutine for t. t.Interval must be > 0,
// which target.Validate() already guarantees for any target that went
// through the normal domain-validated path — but time.NewTicker panics on
// a non-positive duration, so this is checked defensively anyway: a
// TargetLister is an interface boundary the Scheduler doesn't fully
// control (a test double, for instance, could hand back anything), and a
// panic in one per-target goroutine is a worse failure mode than skipping
// that one target and logging why.
func (s *Scheduler) start(ctx context.Context, wg *sync.WaitGroup, running map[string]runningTarget, t target.Target) {
	if t.Interval <= 0 {
		s.logger.Error("scheduler: target has a non-positive interval, skipping", "target_id", t.ID, "target_name", t.Name)
		return
	}

	targetCtx, cancel := context.WithCancel(ctx)
	running[t.ID] = runningTarget{cancel: cancel, target: t}

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.runTarget(targetCtx, t)
	}()

	// Target.URL is deliberately not logged: it can legally contain
	// embedded userinfo credentials (https://user:pass@host/...), and
	// nothing in the domain model forbids that today.
	s.logger.Info("target scheduled", "target_id", t.ID, "target_name", t.Name, "interval", t.Interval)
}

// stop cancels and forgets the running goroutine for id, if any.
func (s *Scheduler) stop(running map[string]runningTarget, id, reason string) {
	rt, ok := running[id]
	if !ok {
		return
	}
	rt.cancel()
	delete(running, id)
	s.logger.Info("target unscheduled", "target_id", id, "reason", reason)
}

// runTarget checks t once immediately — a newly (or newly re-) scheduled
// target is checked right away rather than waiting a full Interval, so a
// target just created (or just edited) through the REST API gets a
// prompt first observation — then checks it again every t.Interval until
// ctx is canceled.
//
// This is a fixed schedule (ticks at t.Interval, 2*t.Interval, ...), not a
// completion-based one (wait t.Interval after each check finishes).
// Completion-based scheduling would let a target's own check latency
// silently slow down how often it gets checked — exactly backwards for a
// monitoring system, since a struggling/slow target is precisely the one
// that should keep being observed at a predictable cadence, not less
// often. A fixed schedule keeps checks evenly spaced regardless of how
// long any individual check takes.
//
// Checks never overlap for a single target. This is not implemented with
// an explicit busy-flag: calling s.runCheck synchronously inside the same
// goroutine that also receives from ticker.C is sufficient, because
// time.Ticker's channel has a buffer of exactly one and drops ticks it
// can't deliver rather than queuing them — documented stdlib behavior, not
// an assumption. So if a check is still running when a tick (or several)
// would have fired, at most one more tick is coalesced and delivered once
// the goroutine is free again; nothing queues up, and nothing overlaps.
// This "skip if still running" policy was chosen over allowing overlapping
// checks because unbounded overlap against a target that is consistently
// slower than its own configured interval would let goroutines and
// in-flight connections for that one target grow without bound — the
// simpler, safer choice for a single-process scheduler. M5's worker pool
// may revisit this with an explicit bounded queue instead, once
// concurrency is bounded by pool size rather than per-target goroutines.
func (s *Scheduler) runTarget(ctx context.Context, t target.Target) {
	s.runCheck(ctx, t)

	ticker := time.NewTicker(t.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCheck(ctx, t)
		}
	}
}

// runCheck performs one check and hands the result to OnResult, if set.
// ctx is the per-target context, itself derived from the context passed
// to Start — so if the Scheduler is shutting down while a check is
// in-flight, the check's own context is canceled too, and checker.Checker
// (M3) reports that promptly as OutcomeCanceled rather than the check
// hanging past shutdown.
func (s *Scheduler) runCheck(ctx context.Context, t target.Target) {
	result := s.checker.Check(ctx, t)

	s.logger.Info("check completed",
		"target_id", t.ID,
		"target_name", t.Name,
		"outcome", result.Outcome,
		"status_code", result.StatusCode,
		"latency", result.Latency,
	)

	if s.onResult != nil {
		s.onResult(result)
	}
}
