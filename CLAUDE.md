# CLAUDE.md

Working memory for future Claude Code sessions on this project. Update this
file at the end of every milestone. It must describe the actual state of
the repository — not planned or aspirational functionality.

## Project Identity

```
Project: InfraVex API Monitor
Organization: InfraVex
Language: Go
Module: github.com/InfraVex/api-monitor
Current Milestone: M5 (complete)
```

## Completed Milestones

```
M0 — Foundation & Architecture
M1 — Domain Model
M2 — Target Management + REST Backend + Docker Foundation
M3 — HTTP Checker
M4 — Scheduler
M5 — Worker Pool + Concurrent Check Execution
```

## Current Architecture

Modular monolith. Full design in `docs/architecture.md` — see "The Worker
Pool (Milestone 5)" for what M5 added.

**What's actually running in `cmd/api-monitor/main.go` as of M5**, all
started together and sharing one shutdown context
(`signal.NotifyContext`):
```
net/http server → RequestID/Logging/Recovery middleware → TargetHandler
       → target.Service → Target.Validate → target.Repository
       → storage.MemoryTargetRepository

worker.Pool.Start(ctx)        (WorkerCount goroutines, bounded queue)

scheduler.Scheduler.Start(ctx)
       → (every DiscoveryInterval) target.Service.List()
       → per enabled target, on its own Interval:
              worker.Pool.Check(ctx, target)   ← was checker.Checker.Check
                                                  directly through M4
                     → Submit: enqueue, block for a free worker,
                       wait for result
                     → pool worker: checker.Checker.Check(ctx, target)
                       (panic-recovered)
                     → CheckResult delivered back to the Scheduler's
                       per-target goroutine
              → OnResult callback (unset — see M5 Implementation)
```

**`internal/scheduler` has zero code changes for M5** (only its "check
completed" log line was removed — see below). `worker.Pool` satisfies
`scheduler.TargetChecker` exactly, so only `main.go`'s wiring changed. This
is the M4 interface decision working as intended.

## Domain Model

Unchanged since M3. See `docs/architecture.md`'s "Domain model (Milestone
1)" and "The HTTP Checker (Milestone 3)" sections.

## M5 Implementation

- **`worker.Pool`** (`internal/worker/pool.go`) — holds only immutable
  config plus the queue channel; all per-run state is local to a `Start`
  call, same shape as `scheduler.Scheduler`.
- **`worker.New(cfg Config) *Pool`** — panics if `Config.Checker` is nil
  (wiring-time validation, same reasoning as `scheduler.New`).
- **`Checker` interface** (`Check(ctx, target.Target) checker.CheckResult`)
  defined in `internal/worker`, consumer-owned like
  `scheduler.TargetChecker`, satisfied by `*checker.Checker` unchanged —
  lets Pool tests inject fast/controllable stubs.
- **`job` struct is unexported** — `target.Target`, `enqueuedAt` (for
  queue-latency logging), and a `result chan checker.CheckResult` buffered
  to capacity 1. The buffer is load-bearing, not cosmetic: without it, a
  worker could block forever delivering a result to a `Submit` caller that
  already gave up due to `ctx` cancellation — a real goroutine-leak risk
  this design specifically avoids.
- **`Pool.Check(ctx, target.Target) checker.CheckResult`** — the method
  that makes `*Pool` a drop-in `TargetChecker`. Internally calls `Submit`;
  on a `Submit` error (submission itself aborted by `ctx`), synthesizes a
  `CheckResult{Outcome: OutcomeCanceled}` via `checker.New` rather than
  ever returning a bare Go error — keeps the same "no separate error
  channel" contract `checker.Checker.Check` established in M3.
- **`Pool.Submit(ctx, target.Target) (CheckResult, error)`** — the real
  API: enqueue (respecting backpressure), then wait for a result, both
  `ctx`-cancellable.
- **Backpressure: block, not drop or coalesce.** Safe specifically because
  of the next point (at most one outstanding job per target), which
  bounds how many goroutines could ever be blocked in `Submit`
  simultaneously by the number of targets, not unbounded. A `Warn` log
  fires once when a submission actually has to wait (queue was full at
  that instant) — not on every normal submission.
- **Duplicate-job policy: at most one outstanding job per target,
  preserved from M4.** `Check` blocking its caller means the Scheduler's
  M4 ticker-coalescing behavior (unchanged) still guarantees this without
  the Scheduler needing to know a pool exists.
- **`WorkerCount` default 10, `QueueSize` default 100** — see
  `internal/worker/pool.go`'s `Config` doc comments for the full
  reasoning (I/O-bound workload, not tied to `runtime.NumCPU()`;
  conservative default since these are third-party APIs InfraVex doesn't
  own).
- **Panic isolation**: `safeCheck` wraps `Checker.Check` in `recover()`.
  Logged loudly (panic value + `debug.Stack()`) at Error; reported to the
  waiting caller as `CheckResult{Outcome: OutcomeConnectionError}` —
  reusing an existing Outcome rather than adding a new domain concept for
  what should be an unreachable safety net.
- **Shutdown: bounded, not a full drain.** Workers stop pulling *new*
  jobs once `ctx` is canceled; an in-flight job finishes (fast, thanks to
  M3's own cancellation handling); anything still queued is abandoned,
  not drained — draining a deep queue could make shutdown itself hang,
  which this project has avoided since M2.
- **Context propagation unchanged from M3.** The Pool passes the same
  `ctx` straight to `checker.Checker.Check` — no extra wrapping layer; the
  per-check timeout is still entirely `target.Timeout`'s job.
- **Scheduler's "check completed" log line was removed** (see
  `internal/scheduler/scheduler.go`'s `runCheck`) — it was duplicating the
  Pool's own "check completed" log one-for-one once the pool sat between
  Scheduler and Checker (**this exact duplication was caught and fixed
  during manual end-to-end testing, not just reasoned about in the
  abstract** — see Verification below). The Pool's version is strictly
  more informative (adds `queue_wait`). The Scheduler's own logging is now
  scoped to scheduling events only (target scheduled/unscheduled).
- **No new REST endpoints** — no `/workers`, `/queue`, `/scheduler`; the
  pool is purely an internal implementation detail, per the M5 brief.
- **Future metrics identified but not built** (per M5 brief, deferred to
  M10): `checks_started`, `checks_completed`, `checks_failed`,
  `queue_depth`, `queue_drops`, `worker_busy`, `check_duration`.

## Configuration

New environment variables (`internal/config/config.go`), same
silent-fallback-to-default philosophy as the existing `HTTP_*` variables:
```
WORKER_COUNT=10    # concurrent checks allowed; must be > 0 or falls back
QUEUE_SIZE=100     # pending-job queue capacity; must be > 0 or falls back
```

## Important Decisions

- **Why bounded concurrency is necessary**: through M4, N enabled targets
  meant up to N simultaneously-running per-target goroutines with no
  shared limit — fine at small scale, but unbounded at 10,000 targets
  (CPU/memory/connection exhaustion, hammering monitored third-party
  APIs). M5 closes this gap.
- **Why the queue is bounded**: an unbounded queue doesn't solve the
  original problem, it just relocates it from "unbounded goroutines" to
  "unbounded queued memory."
- **Why blocking backpressure, not dropping**: safe here specifically
  because per-target submission is already capped at one outstanding job
  (bounding total possible blocked goroutines by target count); dropping
  would create monitoring gaps exactly when the system is under load —
  the worst time for gaps to appear.
- **Why Kafka is still postponed**: nothing in the current architecture
  produces events that need a durable, external broker; M5's in-process
  bounded channel fully solves the concurrency problem M5 exists to
  solve. M9 revisits this once check results need to flow to multiple
  independent consumers (persistence, incidents, alerts) rather than one
  in-process callback.
- **Why `internal/scheduler` has no code changes**: `worker.Pool`
  implements the exact `TargetChecker` interface shape the Scheduler
  already depended on since M4 — validating that M4's interface boundary
  was drawn in the right place.

## Testing

Run from repo root — all actually executed for M5:
```
gofmt -w .                          # ok, only reformatted whitespace
go vet ./...                         # ok
go build ./...                        # ok
go test ./...                          # ok — all packages pass, including
                                        #      10 new worker/pool tests
go test -race ./...                     # ok — no data races
go test -race -count=3 ./...             # ok — 3x repeat, stable
go test -bench=BenchmarkPool_Submit -benchtime=2s -run=^$ ./internal/worker/...
    # ok — ~7.2µs/op pool-submission overhead (20 workers, instant fake
    # checker); confirms the queueing mechanism itself is not a
    # meaningful bottleneck next to real HTTP latencies (milliseconds+)
```
Key worker tests: `TestPool_WorkerLimit` (proves max observed concurrency
== configured `WorkerCount`, exactly, with 20 targets against 5 workers —
the single most important M5 test), `TestPool_QueueCapacity_BlocksThenUnblocks`
(proves `Submit` actually blocks when queue+workers are full, then
proceeds once capacity frees), `TestPool_PanicRecovery` (proves a panic
doesn't crash the worker and a subsequent job still gets processed —
verified under `-race`, including a deliberate unsynchronized-looking
`p.checker` field swap between test phases that is actually safe via the
job-channel happens-before edge), `TestPool_Shutdown`/
`TestPool_CancellationDuringWait` (no hangs, no leaks).

Manually verified end-to-end (not just unit tests): built the binary, ran
it with `WORKER_COUNT=2` against 5 targets pointing at a local slow mock
server (500ms/response), and confirmed via logs that only 2 checks ever
ran concurrently — the first 2 targets had `queue_wait` near zero, the
next 2 waited ~502ms (one full check cycle) for a worker to free up, and
the 5th waited ~1.0s (two cycles). **This manual run is what caught the
duplicate "check completed" logging bug** described above — a good
example of why the manual smoke test step matters beyond what unit tests
alone would have caught (the unit tests didn't assert on log content, so
they couldn't have caught it). Also verified graceful shutdown: `SIGTERM`
produced `worker stopped` (x2) → `worker pool stopped` → `scheduler
stopped` → `scheduler stopped cleanly` → `worker pool stopped cleanly`,
all within the same instant, no hang. (Note: the exact interleaving of
independently-racing goroutines' log lines, e.g. "worker stopped" vs.
"shutdown signal received", varies run to run — expected, since nothing
orders those relative to each other; only the two lines `main.go`
explicitly sequences via blocking channel receives are guaranteed in
order.)

**Docker was not executed** — still no Docker daemon available in this
environment (same as M2/M3/M4). The `Dockerfile` needed no changes for M5
(worker configuration flows through the same environment-variable
mechanism already supported).

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go              (wiring only — now also starts the Pool)
├── internal/
│   ├── target/    target.go, repository.go, service.go, doc.go + tests
│   ├── checker/   check_result.go, check.go, doc.go + tests
│   ├── scheduler/ scheduler.go (log line removed, see above), doc.go + tests
│   ├── worker/    pool.go (NEW M5), doc.go + tests + benchmark
│   ├── incident/  incident.go, doc.go + tests                (M1, still unused)
│   ├── id/        id.go                                      (shared UUID util)
│   ├── config/    config.go (+WorkerCount/QueueSize) + tests
│   ├── storage/   memory_target_repository.go, doc.go + tests
│   ├── api/       server.go, router.go, target_handler.go,
│   │              health_handler.go, middleware.go, response.go,
│   │              target_dto.go, doc.go + tests
│   └── alert/     doc.go only (not implemented)
├── docs/architecture.md, development.md, roadmap.md, api.md
├── Dockerfile, .dockerignore
├── README.md, LICENSE, .gitignore, go.mod, CLAUDE.md
```

## Next Milestone

**M6 — PostgreSQL Persistence + Docker Compose** is next: replace
`storage.MemoryTargetRepository` with a PostgreSQL-backed implementation
of the same `target.Repository` interface (another validation of the same
interface-boundary pattern that made M5 a clean swap), add schema/
migrations, and introduce `docker-compose.yml` — the point at which a
multi-container setup finally has something real to orchestrate (a single
service had nothing to compose against through M5). Also the natural
place to start persisting `CheckResult`s via the `OnResult` callback that
has sat unused since M4.
