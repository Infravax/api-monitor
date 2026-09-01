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
| M5 | Concurrent Workers | Introduce safe concurrency and worker management. |
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

Milestone 4 is complete: `scheduler.Scheduler`
(`internal/scheduler/scheduler.go`) periodically re-lists targets and, for
each enabled one, triggers `checker.Checker.Check` on its own configured
`Interval` — checks never overlap for a single target, new/changed
targets are checked immediately, and disabled/deleted targets stop being
scheduled within one `DiscoveryInterval`. **This is the first milestone
where the application actually runs real HTTP checks**:
`cmd/api-monitor/main.go` now starts the Scheduler alongside the HTTP
server, sharing the same shutdown context.

`CheckResult`s are not yet persisted (M6) or interpreted (M7/M8) — they
currently only reach an optional, unset-by-default `OnResult` callback and
the Scheduler's own lifecycle log line.

M2's target management REST API and M3's Checker are unchanged:
`POST/GET/PUT/DELETE /api/v1/targets` and `GET /health` remain backed by
`target.Service` → `target.Repository` → `storage.MemoryTargetRepository`
(concurrency-safe, in-memory); `checker.Checker` remains a concrete,
stateless, concurrency-safe HTTP/HTTPS checker.
