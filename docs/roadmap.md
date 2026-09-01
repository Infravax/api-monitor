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
| M6 | PostgreSQL Persistence + Docker Compose | Durable `target.Repository` implementation backed by PostgreSQL (replacing the M2 in-memory one for production use), embedded migrations, and `docker-compose.yml` wiring the API service to a real Postgres container. *(done)* |
| M7 | Incident Engine | UP/DOWN state transitions, failure thresholds and recovery. |
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

Milestone 6 is complete: targets created through the REST API now survive
an application restart. `postgres.TargetRepository`
(`internal/storage/postgres`) is a second implementation of
`target.Repository` — PostgreSQL-backed, using `pgx` natively and
`golang-migrate` for embedded, startup-run schema migrations —
alongside the still-present in-memory one from M2. `cmd/api-monitor/main.go`
now wires in the PostgreSQL repository; **`target.Service`,
`internal/api`, and `internal/scheduler` needed zero code changes**, the
same interface-boundary pattern that made M5's worker pool a clean swap.
The application now fails fast with a clear error and non-zero exit if
PostgreSQL is unreachable or migrations fail at startup, rather than
starting in a silently-broken state. `docker-compose.yml` wires the API
service to a real `postgres:16.4` container with a named volume (data
survives `docker compose down`; only `down -v` destroys it) and a
healthcheck-gated startup order.

`CheckResult`s are still not persisted — that needs its own table/
repository, not built in M6 — and nothing interprets them yet (M7/M8).
They currently only reach an optional, unset-by-default `OnResult`
callback and the worker pool's own "check completed" log line (M5).

M2's target management REST API contract, M3's Checker, M4's Scheduler
reconciliation logic, and M5's worker pool are otherwise unchanged:
`POST/GET/PUT/DELETE /api/v1/targets` and `GET /health` behave exactly as
before, now backed by PostgreSQL instead of memory by default.
