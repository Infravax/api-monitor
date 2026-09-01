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
Current Milestone: M4 (complete)
```

## Completed Milestones

```
M0 — Foundation & Architecture
M1 — Domain Model
M2 — Target Management + REST Backend + Docker Foundation
M3 — HTTP Checker
M4 — Scheduler
```

## Current Architecture

Modular monolith. Full design in `docs/architecture.md` — see its "The
Scheduler (Milestone 4)" section for what M4 added.

**What's actually running in `cmd/api-monitor/main.go` as of M4**, started
together and sharing one shutdown context (`signal.NotifyContext`):
```
net/http server → RequestID/Logging/Recovery middleware → TargetHandler
       → target.Service → Target.Validate → target.Repository
       → storage.MemoryTargetRepository

scheduler.Scheduler.Start(ctx)
       → (every DiscoveryInterval) target.Service.List()
       → per enabled target, on its own Interval:
              checker.Checker.Check(ctx, target) → checker.CheckResult
              → OnResult callback (unset — see M4 Implementation)
```

**This is the first milestone where the application performs real HTTP
checks against real targets**, not just as a tested standalone package.
`incident.Incident` (M1) still has no consumer. `alert`, `storage`
(beyond the M2 in-memory target repository) remain doc-only/absent.

## Domain Model

Unchanged since M3. See `docs/architecture.md`'s "Domain model (Milestone
1)" and "The HTTP Checker (Milestone 3)" sections (`Target`, `CheckResult`
with its `OutcomeCanceled` addition, `Incident`).

## M4 Implementation

- **`scheduler.Scheduler`** (`internal/scheduler/scheduler.go`) — holds
  only immutable config; all per-run state (the map of running per-target
  goroutines, a `sync.WaitGroup`) is local to a single `Start` call, so the
  Scheduler itself has no shared mutable state and `Start` could be called
  more than once sequentially on the same instance.
- **`scheduler.New(cfg Config) *Scheduler`** — panics if `Config.Targets`
  or `Config.Checker` is nil (wiring-time validation; this is only ever
  called once from `main.go`, not driven by external input, so failing
  fast beats a confusing nil-pointer panic three calls deep later).
- **Two consumer-defined interfaces** in `internal/scheduler` (same
  pattern as `target.Repository` from M2 — owned by the consumer, not the
  implementer): `TargetLister` (`List(ctx) ([]target.Target, error)`,
  satisfied by `*target.Service` unchanged) and `TargetChecker`
  (`Check(ctx, target.Target) checker.CheckResult`, satisfied by
  `*checker.Checker` unchanged). Justified by a concrete need: scheduler
  tests inject fakes so scheduling-*timing* tests don't need real HTTP
  round-trips. This does **not** reverse M3's decision to keep
  `checker.Checker` itself concrete — that was about the Checker package
  having one implementation; this is a different consumer's need.
- **`Start(ctx context.Context) error`** — the only lifecycle method; no
  separate `Stop`. Blocks until `ctx` is canceled, then returns only once
  every per-target goroutine it started has actually exited
  (`sync.WaitGroup.Wait()`) — no goroutine leaks after return. Mirrors the
  M2 HTTP server's lifecycle model exactly (`main.go` runs both against
  the same top-level `ctx`; one SIGTERM stops both).
- **Discovery/reconciliation, not push-based updates.** Every
  `DiscoveryInterval` (default 10s; also run once immediately at `Start`),
  the Scheduler calls `TargetLister.List` and reconciles: new enabled
  target → start a goroutine; existing target whose full `Target` value
  changed (any field, via direct `==` — `Target` is a plain comparable
  struct) → stop and restart with the new value; disabled or no-longer-
  present (deleted) target → stop. This single mechanism handles target
  create/update/enable/disable/delete uniformly, at the cost of up to
  `DiscoveryInterval` staleness — an explicit, documented trade-off, not
  an oversight.
- **One goroutine + one `time.Ticker` per enabled target** — chosen over a
  central loop or a timer/priority queue as the simplest design that still
  gives every target its own correctly-spaced schedule; a priority queue
  solves a scale problem (many thousands of targets) this milestone
  doesn't have.
- **Fixed schedule** (ticks at `Interval`, `2*Interval`, ...), not
  completion-based (wait `Interval` after each check finishes).
  Deliberate: completion-based scheduling would let a target's own
  latency silently reduce how often it gets checked — backwards for a
  monitoring system.
- **Checks never overlap for one target ("skip if still running").** No
  explicit busy-flag: `runCheck` runs synchronously in the same goroutine
  that receives from `ticker.C`, and `time.Ticker`'s channel (buffer of
  one, drops ticks it can't deliver) does the coalescing for free —
  documented stdlib behavior, not an assumption. Chosen over allowing
  overlap specifically because unbounded overlap against a consistently-
  slow target would let goroutines/connections for that one target grow
  without bound. M5's worker pool may replace this with an explicit
  bounded queue.
- **New or just-changed targets are checked immediately**, not after
  waiting a full `Interval` — matters for both genuinely new targets and
  targets whose config just changed via `PUT` (a config change causes a
  full goroutine restart, which always performs an immediate check with
  the new configuration).
- **Shutdown propagates into in-flight checks.** Per-target context →
  Scheduler's `ctx` → (inside `checker.Checker.Check`, M3) per-request
  context. Canceling the top-level `ctx` cancels an in-flight HTTP request
  too, and M3's Checker reports that promptly as `OutcomeCanceled` rather
  than shutdown hanging.
- **Result handling: `Config.OnResult func(checker.CheckResult)`,
  currently unset in `main.go`.** The only path a result has out of the
  Scheduler today; called synchronously in the per-target goroutine (a
  slow `OnResult` would delay that target's next tick — acceptable now,
  since the only concern is a fast in-process callback; this is the exact
  seam M5's worker pool is expected to replace). No fake/placeholder
  persistence was added — M6 does that.
- **Logging**: target ID and name only, never `Target.URL` — a URL can
  legally embed userinfo credentials
  (`https://user:pass@host/...`) and nothing in the domain model forbids
  that today.
- **No REST endpoints for scheduler control** (no
  `POST /scheduler/start|stop`) — application lifecycle (`main.go`)
  controls the Scheduler, per the M4 brief.

## Important Decisions

See "M4 Implementation" above for the full reasoning behind each; the
short version, matching how M4's brief framed the questions:
- Interval semantics: **fixed schedule**, not completion-based.
- Overlapping checks: **skip if still running**, not allowed to overlap.
- Target source: **`TargetLister` interface over `*target.Service`**, not
  direct repository access.
- Clock/timer testability: **no custom clock abstraction** — standard
  `time.Ticker`/`time.Sleep` with short (tens-of-ms) intervals and
  generous tolerances in tests were sufficient; a clock abstraction had no
  current justified need.
- Checker integration: **consumer-defined `TargetChecker` interface**,
  not a dependency on the concrete `*checker.Checker` type.

## Dependencies

None beyond the Go standard library. `go.mod` has no `require` block, no
`go.sum` exists. (Unchanged since M0.)

## Verification

Run from repo root — all actually executed for M4:
```
gofmt -w .            # ok, only reformatted whitespace/alignment
go vet ./...           # ok
go build ./...          # ok
go test ./...            # ok — all packages pass, including 8 new
                          #      scheduler tests
go test -race ./...       # ok — no data races
go test -race -count=5 ./internal/scheduler/...   # ok — 5x repeat, stable
```
Manually verified end-to-end (not just unit tests): built the binary, ran
it with a locally-created target (via `curl` against the M2 REST API)
pointing at a local `python3 -m http.server`, and confirmed via logs that
the Scheduler discovered the new target, began checking it every 2s as
configured, and that `SIGTERM` produced a clean, ordered shutdown
(`scheduler stopped` → `shutdown signal received` → `http server stopped
cleanly` → `scheduler stopped cleanly`) with no hang. One brief window of
inter-check timing jitter (~5s instead of 2s, twice, in an otherwise
~75-check run) was observed during that manual run — consistent with
ordinary OS/container scheduling delay in this sandbox (`time.Ticker`
doesn't guarantee hard real-time precision), not a bug; the automated
timing tests (tight tolerances, repeated 5x under `-race`) are what
actually validate scheduling correctness.

**Docker was not executed** — still no Docker daemon available in this
environment (same as M2/M3). The `Dockerfile` needed no changes for M4
(same build command, same entrypoint) and was not re-verified beyond that
review.

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go              (wiring only — now also starts the Scheduler)
├── internal/
│   ├── target/    target.go, repository.go, service.go, doc.go + tests
│   ├── checker/   check_result.go, check.go, doc.go + tests
│   ├── scheduler/ scheduler.go (NEW M4), doc.go + tests
│   ├── incident/  incident.go, doc.go + tests                (M1, still unused)
│   ├── id/        id.go                                      (shared UUID util)
│   ├── config/    config.go + tests
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

**M5 — Worker Pool + Concurrent Check Execution** is next: bound the
Scheduler's currently-unbounded per-target-goroutine concurrency (today, N
enabled targets means up to N goroutines with no shared limit) with an
actual worker pool. Expect the Scheduler's role to narrow from "identify
work AND run it" to "identify work due and hand it to the pool" — the M4
brief explicitly designed the Scheduler's responsibility as "identify work
that is due," not "own all concurrent execution forever," anticipating
this. The skip-if-still-running overlap policy from M4 may also be
revisited once concurrency is bounded by pool size rather than per-target
goroutines.
