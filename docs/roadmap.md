# Roadmap

API Monitor is built in small, working milestones (see
[development.md](development.md) principle 2). Each milestone should leave
the project in a state that builds, runs, and can be verified.

| Milestone | Name | Scope |
|---|---|---|
| Milestone | Name | Scope |
|---|---|---|
| M0 | Foundation & Architecture | Project structure, Go setup, documentation, architecture. *(done)* |
| M1 | Domain Model | `Target`, `CheckResult`, `Incident`, and related domain concepts. *(done)* |
| M2 | Target Management + REST Backend + Docker Foundation | CRUD REST API for targets (handler/service/repository), in-memory storage, HTTP server with graceful shutdown, config, middleware, Dockerfile. *(done)* |
| M3 | HTTP Checker | Perform HTTP/HTTPS checks, measure latency, handle timeout/errors. |
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

Milestone 2 is complete: `POST/GET/PUT/DELETE /api/v1/targets` and
`GET /health` are live, backed by `target.Service` →
`target.Repository` → `storage.MemoryTargetRepository`
(concurrency-safe, in-memory). The HTTP server
(`internal/api`, `net/http` only) has request-ID/logging/recovery
middleware, configurable timeouts (`internal/config`), and graceful
shutdown. A `Dockerfile` builds and runs the service. No HTTP checking,
scheduling, persistence beyond memory, incident detection, or alerting
exists yet — those begin at Milestone 3 onward.
