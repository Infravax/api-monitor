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
| M4 | Scheduler | Periodically schedule monitoring checks. |
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

Milestone 3 is complete: `checker.Checker` (`internal/checker/check.go`)
performs one real HTTP/HTTPS request against a `target.Target` and
returns a `checker.CheckResult`, correctly classifying success,
unexpected status, timeout, connection failure, and caller-initiated
cancellation (`OutcomeCanceled`, added this milestone). It is
concurrency-safe (verified with `go test -race`) and fully covered by
`httptest`-based tests — no external network dependency.

**The Checker is not yet invoked by anything in the running
application.** Nothing schedules it periodically (M4), runs it under a
worker pool (M5), persists its output (M6), or reacts to it (M7/M8). It
exists today only as a tested, callable package.

M2's target management REST API remains as previously described:
`POST/GET/PUT/DELETE /api/v1/targets` and `GET /health`, backed by
`target.Service` → `target.Repository` → `storage.MemoryTargetRepository`
(concurrency-safe, in-memory), with request-ID/logging/recovery
middleware, configurable timeouts, graceful shutdown, and a `Dockerfile`.
