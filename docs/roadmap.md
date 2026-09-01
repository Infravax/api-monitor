# Roadmap

API Monitor is built in small, working milestones (see
[development.md](development.md) principle 2). Each milestone should leave
the project in a state that builds, runs, and can be verified.

| Milestone | Name | Scope |
|---|---|---|
| M0 | Foundation & Architecture | Project structure, Go setup, documentation, architecture. *(done)* |
| M1 | Domain Model | `Target`, `CheckResult`, `Incident`, and related domain concepts. *(done)* |
| M2 | Target Management + REST Backend + Docker Foundation | CRUD REST API for targets (handler/service/repository), in-memory storage, HTTP server with graceful shutdown, config, middleware, Dockerfile. *(done)* |
| M3 | HTTP Checker | Perform one real HTTP/HTTPS check against a `Target`, measure latency, classify timeout/connection/status outcomes. *(done)* |
| M4 | Scheduler | Periodically trigger the M3 Checker for each enabled target on its own configured interval; wire it into the running application. *(done)* |
| M5 | Worker Pool + Concurrent Check Execution | Bound total concurrent check execution with a fixed-size worker pool and a bounded queue, replacing the Scheduler's previously-unbounded per-target goroutine execution. *(done)* |
| M6 | Persistence | PostgreSQL, schema design, migrations, repositories (replacing the M2 in-memory one), Docker Compose. |
| M7 | Health & Incident Engine | UP/DOWN state transitions, failure thresholds and recovery. |
| M8 | Alerting | Webhook-based alerts and alert deduplication. |
| M9 | Kafka / Event-driven architecture | Event-driven pipeline for check results/incidents; querying endpoints beyond the target CRUD already built in M2. |
| M10 | Observability | Metrics, tracing, health/readiness endpoints (readiness becomes meaningful once M6 adds a real dependency to check). |
| M11 | Productionization | Configuration hardening, integration tests, and deployment readiness beyond the M2 Docker foundation. |
| M12 | Scaling & Hardening | Load testing, bottleneck analysis, distributed-worker architecture and production hardening. |

This table reflects the plan as revised during M2 (see `CLAUDE.md` for the
reasoning): the target REST API and initial Docker image moved from
M9/M11 into M2 itself, since M2 was the milestone that introduced the HTTP
server they depend on.

## Current status

Milestone 5 is complete: `worker.Pool` (`internal/worker/pool.go`) bounds
how many checks run concurrently — a fixed number of workers
(`WORKER_COUNT`, default 10) pull from a bounded queue
(`QUEUE_SIZE`, default 100). `cmd/api-monitor/main.go` now wires the
Scheduler's `Checker` to the pool instead of directly to
`checker.Checker`; **`internal/scheduler` itself required no code
changes**, since `worker.Pool` implements the same `TargetChecker`
interface the Scheduler has depended on since M4. Backpressure blocks the
submitting goroutine rather than dropping checks; at most one check per
target is ever outstanding (queued or running) at a time, preserved from
M4; a panic during a check is recovered and logged, not left to crash a
worker.

`CheckResult`s are not yet persisted (M6) or interpreted (M7/M8) — they
currently only reach an optional, unset-by-default `OnResult` callback and
the pool's own "check completed" log line (moved there from the
Scheduler this milestone, to avoid double-logging every check once the
pool sat between them).

M2's target management REST API, M3's Checker, and M4's Scheduler
reconciliation logic are otherwise unchanged:
`POST/GET/PUT/DELETE /api/v1/targets` and `GET /health` remain backed by
`target.Service` → `target.Repository` → `storage.MemoryTargetRepository`
(concurrency-safe, in-memory); `checker.Checker` remains a concrete,
stateless, concurrency-safe HTTP/HTTPS checker, now called from a pool
worker instead of directly from the Scheduler.
