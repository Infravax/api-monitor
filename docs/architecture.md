# Architecture

> Status: Milestone 6 — targets are now durably persisted in PostgreSQL
> instead of memory: a target created through the REST API survives an
> application restart. `target.Repository` (M2) gained its second
> implementation, `postgres.TargetRepository`, alongside the still-present
> in-memory one — `target.Service`, `internal/api`, and
> `internal/scheduler` needed no changes to support it, the same
> interface-boundary pattern that made M5's worker pool a clean swap
> rather than a rewrite. `CheckResult`s are still not persisted (that's a
> different table/repository, not built yet) and nothing interprets them
> (M7/M8) — a produced `CheckResult` currently only reaches an optional
> callback, which the application leaves unset today. See
> [PostgreSQL Persistence (Milestone 6)](#postgresql-persistence-milestone-6)
> below for what's actually implemented vs. the target design in
> [Core components](#core-components).

## System overview

API Monitor is a service that periodically checks whether HTTP/HTTPS
endpoints ("targets") are reachable, respond correctly, and respond within
acceptable time, and that keeps a history of those checks so uptime and
incidents can be derived from them.

It is built as a **modular monolith**: a single deployable binary composed
of clearly separated internal packages, each with one responsibility. This
is a deliberate starting point, not a limitation — see
[Future scaling direction](#future-scaling-direction) below.

## Core components

```
Client
   |
   v
API Monitor Service
   |
   +--> Target Management
   |
   +--> Scheduler
   |
   +--> Checker
   |
   +--> Result Processor
   |
   +--> Incident Manager
   |
   +--> Alert Manager
   |
   +--> Storage
```

| Component | Responsibility | Explicitly not responsible for |
|---|---|---|
| Target Management | CRUD and validation of monitored targets (URL, method, expected status, interval). Source of truth for "what to check." | Performing checks, deciding UP/DOWN |
| Scheduler | Deciding *when* each target's next check is due, based on its interval. | Making HTTP requests, interpreting results |
| Checker | Performing one HTTP/HTTPS request against a target; measuring latency, status, timeouts, connection errors. | Deciding whether this constitutes an incident |
| Result Processor | Normalizing a raw check outcome into a stored `CheckResult`; routing it onward. | Alert delivery |
| Incident Manager | Applying failure/recovery thresholds to a stream of results; opening and resolving incidents; owning UP/DOWN state. | Sending notifications |
| Alert Manager | Notifying external systems (initially webhooks) when incident state changes; deduplicating repeated notifications. | Deciding what counts as a failure |
| Storage | Persisting targets, check results, and incidents; serving historical queries. | Business rules about what data means |

Each component has a single reason to change. That is what keeps the
monolith modular instead of tangled, and it is what allows any one
component (most likely Scheduler + Checker) to be pulled out into a
separately deployable service later without rewriting the others.

## Data flow

```mermaid
flowchart TD
    Client --> Monitor
    Monitor --> Scheduler
    Scheduler --> Checker
    Checker --> TargetAPI[Target API under test]
    Checker --> ResultProcessor
    ResultProcessor --> Storage
    ResultProcessor --> IncidentManager
    IncidentManager --> AlertManager
    IncidentManager --> Storage
```

Walking through it:

1. A **target** is registered through Target Management (eventually via the
   REST API).
2. The **Scheduler** determines the target is due for a check and triggers
   the **Checker**.
3. The **Checker** performs the HTTP/HTTPS request against the real target
   API and records latency, status code, and any error/timeout.
4. The **Result Processor** turns that raw outcome into a `CheckResult` and
   sends it to **Storage** and to the **Incident Manager**.
5. The **Incident Manager** applies threshold rules (e.g., N consecutive
   failures) to decide if a target's state should flip between UP and
   DOWN, and records incident open/resolve events in Storage.
6. On a state transition, the **Incident Manager** notifies the **Alert
   Manager**, which delivers a notification (e.g., a webhook call).
7. The **Client** (a person or another InfraVex tool) reads current state
   and history back out through the REST API, backed by Storage.

## Future scaling direction

The modular monolith is intentionally structured so that scaling out later
is a matter of changing *wiring*, not rewriting logic:

- **Scheduler + Checker** are the natural first extraction point: they can
  become one or more independent **worker** processes that pull due checks
  from a **queue**, so check volume can scale independently of the rest of
  the service.
- Workers could run from multiple **regions**, each reporting results back
  through the same `CheckResult` contract, enabling multi-region
  monitoring without changing how results are interpreted downstream.
- **Storage** already sits behind package boundaries used only through
  interfaces defined by their consumers, so the backing store can change
  (or be split, e.g. hot recent-results store vs. cold historical store)
  without touching business logic.
- **Incident Manager** and **Alert Manager** stay centralized longer, since
  incident state needs a single source of truth even if checks become
  distributed.

None of this distributed infrastructure is built now. It is a direction the
package boundaries are chosen to keep open, not a plan being implemented in
Milestone 0.

## Domain model (Milestone 1)

Three domain types now exist, each owned by the package most responsible
for producing or acting on it:

```
Target
   │
   │ monitored by (Scheduler triggers Checker against it — M3/M4)
   ↓
CheckResult
   │
   │ contributes to (Incident Manager applies thresholds — M7)
   ↓
Incident
```

- **`Target`** (`internal/target`) — an HTTP/HTTPS endpoint to check, plus
  the policy for checking it (method, interval, timeout, expected status).
- **`CheckResult`** (`internal/checker`) — the outcome of one check
  attempt. Represented via a stored `Outcome` enum (`success`,
  `unexpected_status`, `timeout`, `connection_error`) rather than a raw
  `Success` bool, so a result can't represent a contradictory state (e.g.
  "successful" with an error message attached). `Success()` is derived from
  `Outcome`.
- **`Incident`** (`internal/incident`) — a period during which a target is
  considered unhealthy. Open vs. resolved is represented solely by whether
  `ResolvedAt` is nil, not a separate status field, for the same
  no-contradictory-state reason. Resolving is a one-way transition in this
  milestone: a resolved incident is terminal, and a later recurrence
  becomes a new incident rather than reopening the old one. The
  failure/recovery *threshold* logic that decides *when* to open or resolve
  an incident belongs to the Incident Manager (M7) — this milestone only
  defines the shape and the open→resolved transition itself.

These three packages currently have no dependencies on each other: IDs
crossing between them (e.g. `CheckResult.TargetID`) are plain `string`
values rather than a shared cross-package type, keeping the packages
independently testable. A small `internal/id` leaf package (UUIDv4 via
`crypto/rand`, no third-party dependency) is shared by all three
constructors purely to avoid duplicating ID-generation code; it carries no
business meaning of its own.

## Request architecture (Milestone 2, storage updated in Milestone 6)

This is what's actually running today for target management — a real
subset of the [Core components](#core-components) design above (Target
Management + Storage), not the full future system:

```
                    Client
                      │
                      ▼
                HTTP Server (net/http, internal/api)
                      │
        Middleware: RequestID → Logging → Recovery
                      │
                      ▼
                TargetHandler (internal/api)
                      │
                      ▼
                target.Service (internal/target)
                      │
                      ▼
                Target domain type + Validate (internal/target, M1)
                      │
                      ▼
                target.Repository interface (internal/target)
                      │
                      ▼
        postgres.TargetRepository (internal/storage/postgres) — used by
        main.go as of M6; MemoryTargetRepository (internal/storage) still
        exists and satisfies the same interface, used only where an
        in-memory implementation is genuinely useful (e.g. handler tests
        that don't want a database dependency)
```

Result Processor, Incident Manager, and Alert Manager from the
[Core components](#core-components) table above are **not** wired into
this flow yet — they remain future components. The Scheduler and Checker
exist as of M4/M3 (see below), but neither is part of this HTTP request
path at all — the Scheduler runs as its own independent loop, started
alongside the HTTP server in `main.go`, not triggered by any REST request.

**Durable storage arrived in M6.** Through M5, `MemoryTargetRepository`
(a `map[string]Target` behind a mutex) was the only implementation, and
data was lost on every restart. As of M6, `main.go` wires in
`postgres.TargetRepository` instead — a target created through the REST
API now survives an application restart (verified in
`internal/storage/postgres/e2e_test.go`'s `TestE2E_DataSurvivesRestart`,
against a real PostgreSQL instance, not a mock). Both implementations
satisfy `target.Repository` — an interface defined by `internal/target`
(the consumer), not by `internal/storage` — identically, so `target.Service`
and `internal/api` needed zero changes to support the swap. See
[PostgreSQL Persistence (Milestone 6)](#postgresql-persistence-milestone-6)
below for the new implementation's own design.

The **`Handler → Service → Repository`** split mirrors the same
single-reason-to-change principle as the top-level component table:
`TargetHandler` only knows HTTP (status codes, JSON, path parsing);
`target.Service` only knows application flow (fetch-then-validate-then-save
for updates, defaulting for creates) and is unaware `net/http` exists;
`Target.Validate` (M1) remains the single source of truth for what makes a
target valid — the service and handler both defer to it rather than
re-implementing validation rules at their own layer.

Configuration (`internal/config`) and cross-cutting HTTP middleware
(request ID, structured logging, panic recovery — `internal/api`) round
out the M2 additions; both are described in `docs/development.md` and
`docs/api.md`.

## The HTTP Checker (Milestone 3)

```
Target
   │
   ▼
checker.Checker.Check(ctx, target)
   │
   ▼
HTTP/HTTPS request (net/http, context-bounded by target.Timeout)
   │
   ▼
External API under test
   │
   ▼
Response / error
   │
   ▼
CheckResult (Outcome: success | unexpected_status | timeout |
             connection_error | canceled)
```

`checker.Checker` (`internal/checker/check.go`) answers exactly one
question — *"what happened when we attempted this request?"* — and
nothing else. It is deliberately kept separate from, and unaware of, the
**Incident Manager**: the Checker reports an observation; deciding what a
sequence of observations *means* for a target's overall UP/DOWN state is
the Incident Manager's job (M7), not the Checker's. This mirrors the
`Handler`/`Service` split from M2: each layer answers one question and
defers judgment calls to whichever layer actually owns them.

Key design points (see `internal/checker/check.go`'s doc comments for the
full reasoning):

- **Concrete type, not an interface.** There is exactly one
  implementation of "perform an HTTP check," and no current caller needs
  to substitute a different one — an interface here would be abstraction
  without a reason (`docs/development.md`, principle 6).
- **Per-check context timeout, not `http.Client.Timeout`.** A single
  `*http.Client` is shared across targets with different configured
  `Timeout` values, so the timeout budget is applied per call via
  `context.WithTimeout(ctx, target.Timeout)`, not as a single fixed value
  on the client.
- **`OutcomeCanceled` (new in M3).** The M1 `Outcome` enum could not
  distinguish "the target's own configured timeout elapsed" from "the
  caller aborted the check for operational reasons" (e.g. process
  shutdown). Conflating them would corrupt future incident-detection data
  by misreporting an operational abort as target slowness, so one new
  outcome was added — the smallest domain change that made the
  distinction representable, not a redesign of `CheckResult`.
- **Standard redirect/TLS behavior.** Redirects follow `net/http`'s
  default policy (most real APIs redirect at least once, e.g. http→https);
  TLS uses the default system trust store with no
  `InsecureSkipVerify` — a certificate failure is a real monitoring
  failure, reported as `connection_error`.
- **Bounded body draining.** The response body is drained (capped at 64
  KiB) via `io.Discard` and closed, so the underlying connection can be
  reused on a future check against the same target, without ever buffering
  the content — M3 does not validate response bodies.
- **No persistence, scheduling, or logging inside the Checker.** It
  returns a value; what happens to that value (stored, scheduled again,
  logged, alerted on) is entirely the caller's decision. As of M4, that
  caller is the Scheduler (below) — the Checker package itself is
  unchanged by that; it still doesn't know the Scheduler exists.

This does not change the [Future scaling direction](#future-scaling-direction)
section above — `Checker.Check` was written statelessly and safely for
concurrent use specifically so that M5's worker pool can call it from many
goroutines without modification. That is a property that already holds
today (verified with `go test -race`), not a promise about work still to
come.

## The Scheduler (Milestone 4)

```
                  target.Repository
                         │
                         ▼
                    target.Service
                         │
                         ▼ (re-listed every DiscoveryInterval)
                     Scheduler
                         │
              target due (per-target ticker)
                         │
                         ▼
               worker.Pool.Check(ctx, target)  (M5 — see below;
                         │                      was checker.Checker.Check
                         │                      directly through M4)
                         ▼
                    CheckResult
                         │
                         ▼
        OnResult callback (unset today — see below)
```

`scheduler.Scheduler` (`internal/scheduler/scheduler.go`) answers one
question — *"when is each enabled target due for its next check?"* — and
triggers a check when the answer is "now," via whatever `TargetChecker` it
was configured with. It does not perform HTTP requests itself, does not
interpret a `CheckResult` at all (as of M5 it doesn't even log one — see
[The Worker Pool](#the-worker-pool-milestone-5) below for why), and does
not touch storage internals directly. This is the milestone where the
request-facing side of the application (Target Management's REST API, M2)
and the monitoring side (Checker, M3) were first connected — through the
Scheduler, not through each other. That connection point is unchanged in
M5; only what sits *behind* `TargetChecker` changed.

**This is the first milestone where something in the running application
actually calls the Checker.** `cmd/api-monitor/main.go` constructs a
`scheduler.Scheduler` and starts it (`go sched.Start(ctx)`) alongside the
HTTP server, sharing the same top-level context from `signal.NotifyContext`
— one SIGTERM/SIGINT stops both.

Key design points (see `internal/scheduler/scheduler.go`'s doc comments
for the full reasoning):

- **Two consumer-defined interfaces, not a dependency on concrete types.**
  `TargetLister` (`List(ctx) ([]target.Target, error)`) and
  `TargetChecker` (`Check(ctx, target.Target) checker.CheckResult`) are
  defined in `internal/scheduler` itself — the same "interface owned by
  the consumer, not the implementer" pattern `target.Repository`
  established in M2. `*target.Service` and `*checker.Checker` already
  satisfy them with zero changes. This does not reverse M3's decision to
  keep `checker.Checker` a concrete type with no interface of its own
  (that was about the Checker having only one implementation); it's a
  different, later decision by a different consumer, justified by a
  concrete current need: scheduler tests that verify *timing* behavior
  without every test needing a real HTTP round-trip.
- **Polling-based target discovery, not a push/event mechanism.** The
  Scheduler re-lists all targets from `TargetLister` every
  `DiscoveryInterval` (default 10s) and reconciles the set of running
  per-target goroutines against what it finds — starting new ones,
  restarting changed ones, stopping disabled or deleted ones. This is what
  makes target create/update/enable-disable/delete "just work" without
  bespoke handling for each case, at the cost of an explicit, bounded
  staleness window (up to `DiscoveryInterval`) between a change made
  through the REST API and the Scheduler noticing it. A push mechanism
  (e.g. `target.Service` notifying the Scheduler directly) would remove
  that window but adds real coupling/complexity nothing in M4 justifies
  yet.
- **One goroutine per enabled target**, each running its own
  `time.Ticker` at that target's own `Interval` — not a central loop or a
  timer/priority queue. Simplest design that still gives every target its
  own independent, correctly-spaced schedule; a priority queue solves a
  scaling problem (many thousands of targets in one process) this
  milestone doesn't have, and which M12 (distributed scaling), not M4, is
  where that would be revisited if it ever becomes real.
- **Fixed schedule, not completion-based.** Checks happen at
  `t.Interval`, `2*t.Interval`, ... regardless of how long any individual
  check took — not "wait `Interval` after the previous check finishes."
  Completion-based scheduling would let a target's own latency silently
  reduce how often it gets checked, which is backwards for a monitoring
  system: a struggling target is exactly the one that should keep being
  observed on a predictable cadence, not less often.
- **Checks never overlap for a single target ("skip if still running"),
  not allowed to run concurrently.** Not implemented with an explicit
  busy-flag: `runCheck` is called synchronously in the same goroutine that
  receives from `ticker.C`, and `time.Ticker`'s channel has a buffer of
  exactly one and drops ticks it can't deliver — documented stdlib
  behavior, not an assumption. So a check that outlives its own interval
  simply coalesces the ticks that arrive while it's running, rather than
  stacking up concurrent HTTP requests against the same slow/stuck target.
  **This invariant is unchanged and still holds in M5** even though
  execution moved to a pool worker: `TargetChecker.Check` still blocks the
  per-target goroutine until a result is available (see
  [The Worker Pool](#the-worker-pool-milestone-5)), so the same
  ticker-coalescing behavior still guarantees at most one outstanding
  check per target — the Scheduler didn't need to change to preserve it.
- **New/changed targets are checked immediately**, not after waiting a
  full `Interval` — a target just created or edited through the REST API
  gets a prompt first observation rather than waiting up to `Interval`
  (which could be minutes).
- **Shutdown propagates into in-flight checks.** A per-target goroutine's
  context is derived from the Scheduler's own `ctx`, which the Checker's
  own per-request context (M3) is in turn derived from — so when the
  Scheduler shuts down mid-check, that check's HTTP request is canceled
  too, and the M3 Checker reports it as `OutcomeCanceled` promptly rather
  than the shutdown hanging on an in-flight request. `Start` returns only
  once every per-target goroutine it started has actually exited
  (`sync.WaitGroup`), so no goroutines outlive a completed shutdown.
- **Result handling: an optional callback, not a fake persistence
  layer.** `Config.OnResult func(checker.CheckResult)` is the only path a
  `CheckResult` currently has out of the Scheduler. `main.go` leaves it
  unset today — there is no real consumer yet (M6 persistence, M7/M8
  interpretation), and building a placeholder sink would be exactly the
  kind of "pretend it works" code this project avoids. It is still called
  synchronously in the per-target goroutine in M5 (unchanged from M4), so
  a slow `OnResult` would still delay that target's own next tick.
- **The Scheduler no longer logs check results (changed in M5).** Through
  M4 it logged `Outcome`/`status_code`/`latency` itself; that line was
  removed because it duplicated the Worker Pool's own "check completed"
  log line one-for-one, and the Pool's version carries more information
  (queue wait time) than the Scheduler has visibility into. The
  Scheduler's own logging is scoped to scheduling events now (target
  scheduled/unscheduled); execution-result logging belongs to whichever
  `TargetChecker` actually ran the check.
- **URLs are not logged.** Scheduler log lines include target ID and name,
  never `Target.URL` — a URL can legally carry embedded userinfo
  credentials (`https://user:pass@host/...`), and nothing in the domain
  model forbids that today.

Not built by the Scheduler itself, on purpose: PostgreSQL, Kafka, Redis,
or any check execution logic — bounding total concurrent checks across all
targets was the one gap M4 explicitly left open (up through M4, N enabled
targets meant up to N concurrently-running per-target goroutines with no
shared limit), and closing it is exactly what M5's Worker Pool does, below
— without changing a single line of this package.

## The Worker Pool (Milestone 5)

```
                       Targets
                          │
                          ▼
                     Scheduler
                          │
                     target due
                          │
                          ▼
                  ┌────────────────┐
                  │   Work Queue    │   bounded: QueueSize slots
                  │ (buffered chan) │   (default 100)
                  └────────┬────────┘
                            │
              ┌─────────────┼─────────────┐
              ▼             ▼             ▼
          Worker 1      Worker 2  ...  Worker N   N = WorkerCount
              │             │             │        (default 10)
              └─────────────┼─────────────┘
                            ▼
                    checker.Checker
                            │
                            ▼
                       CheckResult
                            │
                            ▼
              (delivered back to the calling
               per-target goroutine in Scheduler)
```

`worker.Pool` (`internal/worker/pool.go`) answers *"how many checks may
run at the same time?"* — a question the Scheduler answered implicitly
(and unboundedly) through M4. It does not decide when a target is due
(Scheduler), does not know how an HTTP check is performed (`checker`), and
does not interpret a result (future Incident Engine, M7).

**The Scheduler required zero code changes for M5.** `worker.Pool`
implements `Check(ctx context.Context, t target.Target) checker.CheckResult`
— exactly `scheduler.TargetChecker`'s signature, the same interface the
Scheduler has depended on since M4. `main.go` is the only thing that
changed: it now constructs a `*worker.Pool` (wrapping a real
`*checker.Checker`) and hands *that* to `scheduler.Config.Checker` instead
of the bare Checker. This is the M4 interface decision paying off exactly
as intended — a swappable implementation, not a rewrite.

Key design points (see `internal/worker/pool.go`'s doc comments for the
full reasoning):

- **Bounded queue (`chan job`, capacity `QueueSize`), not unlimited.** An
  unbounded queue would just relocate the original problem — unbounded
  goroutines — into unbounded queued memory instead of solving it.
- **`WorkerCount` defaults to 10, deliberately not `runtime.NumCPU()`.**
  This is an I/O-bound workload (waiting on network round-trips to
  third-party APIs), not CPU-bound computation, so tying concurrency to
  core count would be arbitrary and likely too low. 10 is a conservative
  starting point specifically because these requests target APIs InfraVex
  doesn't own — a fresh deployment shouldn't hammer someone else's API by
  default. Both `WorkerCount` (`WORKER_COUNT`) and `QueueSize`
  (`QUEUE_SIZE`, default 100) are environment-configurable
  (`internal/config`), with the same silent-fallback-to-default
  philosophy as the rest of the config system for invalid values.
- **Backpressure policy: block the submitting goroutine, not drop or
  coalesce the job.** When the queue is full, `Submit` blocks until room
  frees up (or `ctx` is canceled). This is deliberately safe *because* of
  the next point — per-target submission is already capped at one
  outstanding job, so the number of goroutines that could ever be blocked
  in `Submit` is bounded by the number of targets, never unbounded.
  Dropping was rejected because it creates gaps in monitoring history
  exactly when the system is under load — precisely when observability
  matters most. A future durable queue (Kafka, M9) could absorb bursts
  without an in-process blocking caller at all; nothing in M5 needs that
  yet.
- **Duplicate-job policy: at most one outstanding job per target,
  preserved from M4 — not multiple jobs queued for the same target.**
  `Pool.Check` blocks its caller until a result arrives, so the
  Scheduler's per-target goroutine (and its M4 ticker-coalescing
  behavior) never proceeds to a second submission for the same target
  while the first is still queued or executing. This is also what
  provides fairness between targets "for free": since no target can ever
  occupy more than one queue/worker slot at a time, a high-frequency
  target can't starve a low-frequency one out of proportion to its own
  interval — no priority scheduler was needed.
- **Panic isolation.** `Checker.Check` runs inside a `recover()`. A
  recovered panic is logged loudly (panic value + stack trace) at Error —
  never hidden as a normal check failure — but is reported to the waiting
  caller as an ordinary `CheckResult` (`OutcomeConnectionError`, reusing
  an existing Outcome rather than adding a new domain concept for what
  should be an unreachable safety net) so the worker goroutine survives
  and keeps processing subsequent jobs. A failed *check* (timeout, 500,
  connection refused) was never a threat to the worker in the first place
  — it's normal data, not an error path.
- **Shutdown: bounded, not a full drain.** Once `ctx` is canceled, workers
  stop pulling *new* jobs; a job already executing is allowed to finish
  (which resolves quickly on its own, since `checker.Checker` aborts an
  in-flight HTTP request promptly on cancellation). Jobs still sitting in
  the queue are abandoned, not drained to completion — draining a
  potentially-deep queue could make shutdown take as long as the queue is
  deep, risking the same "must not hang" failure this project has avoided
  since M2. A dropped-on-shutdown check is simply attempted again on the
  target's next scheduled interval after restart.
- **Context propagation unchanged from M3/M4.** The Pool passes the same
  `ctx` straight through to `checker.Checker.Check`, adding no extra
  wrapping layer — each check's own bounded timeout is still entirely
  `target.Timeout`'s job (M3), not something the Pool re-implements.

Identified but deliberately not built in M5 (documented here so M10 has a
starting list, not implemented now): `checks_started`, `checks_completed`,
`checks_failed`, `queue_depth`, `queue_drops`, `worker_busy`,
`check_duration` as real metrics (Prometheus or otherwise) — M5 only logs
discrete events (`worker pool started/stopped`, `worker started/stopped`,
`check completed`, and a `Warn` when a submission has to wait for queue
capacity), which is enough for now but not a substitute for actual
observability tooling.

## PostgreSQL Persistence (Milestone 6)

```
                       REST API (internal/api)
                          │
                          ▼
                    target.Service (internal/target)
                          │
                          ▼
                  target.Repository interface (internal/target)
                          │
                          ▼
               postgres.TargetRepository (internal/storage/postgres)
                          │
                          ▼
                    PostgreSQL (targets table)
```

```
        internal/scheduler  ──────▶  worker.Pool  ──────▶  checker.Checker
   (target.Service.List() reaches   (unchanged, M5)          (unchanged, M3)
    the same PostgreSQL-backed
    repository above — nothing
    else in this chain touches
    the database)
```

`postgres.TargetRepository` (`internal/storage/postgres`) answers the
same question `storage.MemoryTargetRepository` (M2) already answered — how
to store and retrieve `target.Target` values — just durably. It implements
`target.Repository` and nothing more: no SQL leaks into `internal/api`,
`internal/target`, or `internal/scheduler`, matching the M6 brief's
explicit requirement that handlers and the scheduler never execute SQL
directly.

**How persisted targets reach the Scheduler**: no special "load on
startup" step was added. The Scheduler already re-lists targets via
`TargetLister.List()` — once immediately when `Start` is called, and
every `DiscoveryInterval` after that (M4) — so the very first
reconciliation pass after a restart naturally returns whatever was
persisted before the restart. This is the second time a later milestone's
requirement (M6: "targets must be available to the Scheduler on startup")
turned out to already be satisfied by an earlier milestone's design (M4's
polling-based discovery), without new code — the same story as M5's
worker pool needing no `internal/scheduler` changes.

Key design points (see `internal/storage/postgres/*.go`'s doc comments
for the full reasoning):

- **Driver: `pgx` (`github.com/jackc/pgx/v5`), used natively — not
  wrapped in `database/sql`.** PostgreSQL has no standard-library driver,
  so unlike every dependency decision before M6, a third-party one is
  actually justified here (see `docs/development.md`, principle 3). `pgx`
  was chosen over the older `lib/pq` (in maintenance mode, not
  recommended for new projects) as the actively-maintained, de facto
  standard choice, with first-class `context.Context` support and its own
  connection pool (`pgxpool`). Going native rather than through
  `database/sql` avoids an abstraction layer with no current purpose —
  nothing in this project needs to swap PostgreSQL for a different
  database. The dependency surface was checked with `go list -deps`, not
  assumed: pgx itself pulls in only a handful of small, purpose-built
  packages (connection pooling, `.pgpass` support), not a large tree.
- **Migrations: `golang-migrate/migrate`, embedded, run at application
  startup.** A real migration tool, not a hand-rolled one — per the M6
  brief's explicit instruction. Migration SQL files live in
  `internal/storage/postgres/migrations/` and are embedded into the
  compiled binary via `//go:embed`, so the Docker image and `go run` both
  work with zero extra file-mounting or path configuration. Migrations
  run automatically each time the application starts
  (`postgres.Migrate`), before the repository is constructed — simplest
  possible design (schema is always in sync with what the running binary
  expects, no separate manual step to forget) at the scale this project
  operates at. `migrate`'s own `ErrNoChange` (nothing pending) is treated
  as success, so this is safe to run on every startup, not just the
  first.
- **Migrations use a separate, short-lived `database/sql` connection
  from the application's long-lived `pgxpool.Pool`.** `golang-migrate`'s
  database driver is built on `database/sql`, so migrations open their
  own connection via `pgx/v5/stdlib` (which self-registers a `database/sql`
  driver), run once, and close — entirely separate from the
  `pgxpool.Pool` constructed afterward for the application's actual
  runtime queries. There's no reason for a one-time startup step and a
  process-lifetime connection pool to share a connection.
- **Schema maps `target.Target` field-for-field — no invented columns.**
  `id UUID`, `name/url/method TEXT`, `interval_ns/timeout_ns BIGINT`
  (raw nanoseconds — `time.Duration`'s own unit, so there's no
  conversion step or precision loss), `expected_status_code INTEGER`,
  `enabled BOOLEAN`. Verified empirically (not assumed) that `pgx` scans
  a `uuid` column directly into a Go `string` with an exact round trip.
  Deliberately **no** `created_at`/`updated_at` columns: the Go domain
  type doesn't have them (a decision M1 made explicitly and M6's own
  brief reinforced — "do not invent fields that do not exist"), and
  columns nothing in the application reads or writes would be exactly the
  kind of speculative addition this project avoids elsewhere.
- **Database-level constraints protect structural invariants, not
  business rules.** `NOT NULL` on every column, `CHECK` constraints on
  the numeric ranges that are already domain invariants
  (`interval_ns > 0`, `timeout_ns > 0`, `expected_status_code BETWEEN 100
  AND 599`) as defense-in-depth against direct/buggy writes, and the
  primary key for uniqueness. Deliberately **not** duplicated: `method`'s
  allowed-verb set (GET/POST/etc.) — that's `Target.Validate`'s (M1) job,
  re-litigating it as a `CHECK` constraint would just be two sources of
  truth for the same rule. No `UNIQUE` constraint on `name` or `url`
  either — nothing in the Go domain model requires target names or URLs
  to be unique, so adding one at the database layer would invent a new
  rule, not enforce an existing one.
- **Error mapping keeps PostgreSQL-specific errors inside the repository.**
  A unique-violation (SQLSTATE `23505`, checked via the well-known
  `jackc/pgerrcode` constant — already an unavoidable transitive
  dependency of `golang-migrate`'s own pgx driver, so using it directly
  here adds no new dependency surface) maps to `target.ErrAlreadyExists`;
  `pgx.ErrNoRows` maps to `target.ErrNotFound`. `target.Service` never
  needs to know it's talking to PostgreSQL.
- **No transactions.** Every repository method is a single SQL statement
  (`INSERT`, one `SELECT`, `UPDATE`, `DELETE`) — there is no
  multi-statement operation in M6 that needs multiple writes to succeed
  or fail together, so a transaction would add ceremony with no current
  correctness benefit. `Update`/`Delete` check `RowsAffected() == 0` to
  detect "no such row" rather than a separate existence check, avoiding a
  check-then-act race a transaction-free design might otherwise have.
- **Explicit `ORDER BY name, id` for `List`, not PostgreSQL's incidental
  row order.** Kept identical to `MemoryTargetRepository`'s own ordering
  (name, then id — see that type's doc comment for why: the domain model
  has no creation timestamp to order by instead) so the REST API's `GET
  /api/v1/targets` behaves the same regardless of which repository
  backs it.
- **One index: the implicit primary key index on `id`.** No index was
  added to support `ORDER BY name, id` — a full-table scan and sort is
  entirely appropriate at this milestone's scale, and optimizing a query
  with no measured cost problem would be premature. An index on
  `enabled` is a plausible future need once the Scheduler queries "only
  enabled targets" directly in SQL instead of filtering in Go after a
  full `List()` — it doesn't do that today, so it isn't added yet.
- **Connection pool: `MaxConns = 10`, not exposed as an environment
  variable yet.** M6's actual database load is REST API target CRUD plus
  the Scheduler's periodic `List()` calls — comparatively low-volume; the
  worker pool and checker don't touch the database at all. An explicit,
  documented constant was chosen over silently relying on `pgxpool`'s own
  default (`max(4, runtime.NumCPU())`) so the reasoning is visible; it
  isn't a new config knob because nothing has yet demonstrated a need to
  tune it (`docs/development.md`, principle 6).
- **`NewPool` fails fast, with a bounded connectivity check.** A `Ping`
  with a 5-second timeout runs before `NewPool` returns, so a
  misconfigured or unreachable database is caught at startup —
  `main.go` logs the error and exits non-zero — rather than the
  application starting and silently failing the first real request that
  touches storage. Verified directly: stopping PostgreSQL and starting
  the application produces an immediate, clear failure and non-zero exit,
  not a hang or a silent partial start.
- **Graceful shutdown order: HTTP server → Scheduler → worker pool →
  database pool.** The database pool is closed last, deliberately, since
  `target.Service` (reached only through the HTTP server, already
  stopped by that point, and through the Scheduler, also already
  stopped) is the only consumer of it in this process — the worker pool
  and checker never touch the database, so there's no risk of closing
  the pool while something still needs it. Verified by manually starting
  the application, sending `SIGTERM`, and confirming the log order:
  `worker pool stopped` → `scheduler stopped cleanly` → `worker pool
  stopped cleanly` → `database connection pool closed`.
- **Docker Compose exists now because there is finally something to
  orchestrate.** `docker-compose.yml` defines `postgres` (pinned to
  `16.4`, not `latest`; named volume `postgres_data` for data that
  survives `docker compose down` — only `down -v` destroys it; a
  `pg_isready` healthcheck) and `api-monitor`, with `depends_on:
  condition: service_healthy` so Compose itself — not custom
  wait-for-it/retry logic — ensures the application container never
  starts trying to connect before PostgreSQL is actually ready. Real
  secrets are never committed: `.env.example` holds placeholders only,
  `.env` is gitignored.
- **`/health` is unchanged — still pure liveness, not made
  PostgreSQL-aware.** This is a deliberate scope decision, not an
  oversight: `docs/api.md` already flagged in M2 that a readiness check
  was deferred until there was a real dependency to be "not ready" for;
  now there is one, but adding a proper liveness/readiness split is left
  to M10 (Observability) rather than folded into M6. Container startup
  ordering (the actual problem a readiness check would solve here) is
  already handled by Compose's `depends_on: condition: service_healthy`
  on the *postgres* container, which doesn't require the application's
  own health endpoint to change at all.

Not built in M6, on purpose: persisting `CheckResult`s (the `OnResult`
callback from M4/M5 remains unset — that needs its own table/repository,
not a repurposing of `TargetRepository`), Kafka, an ORM, connection-pool
tuning exposed as configuration, and any change to `target.Repository`'s
interface itself (M6 was a pure implementation swap).
