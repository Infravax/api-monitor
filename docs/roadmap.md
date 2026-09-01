# Roadmap

API Monitor is built in small, working milestones (see
[development.md](development.md) principle 2). Each milestone should leave
the project in a state that builds, runs, and can be verified.

| Milestone | Name | Scope |
|---|---|---|
| M0 | Foundation & Architecture | Project structure, Go setup, documentation, architecture. *(current)* |
| M1 | Domain Model | `Target`, `CheckResult`, `Incident`, and related domain concepts. |
| M2 | Target Management | Create, read, update, delete and validate monitoring targets. |
| M3 | HTTP Checker | Perform HTTP/HTTPS checks, measure latency, handle timeout/errors. |
| M4 | Scheduler | Periodically schedule monitoring checks. |
| M5 | Concurrent Workers | Introduce safe concurrency and worker management. |
| M6 | Persistence | PostgreSQL, schema design, migrations and repositories. |
| M7 | Health & Incident Engine | UP/DOWN state transitions, failure thresholds and recovery. |
| M8 | Alerting | Webhook-based alerts and alert deduplication. |
| M9 | REST API | Expose APIs for managing monitoring targets and querying results. |
| M10 | Observability | Structured logging, metrics, health/readiness endpoints. |
| M11 | Productionization | Docker, configuration, graceful shutdown, integration tests and deployment readiness. |
| M12 | Scaling & Hardening | Load testing, bottleneck analysis, distributed-worker architecture and production hardening. |

## Current status

Milestone 0 is complete: the repository, Go module, package skeletons, and
documentation exist. No monitoring functionality is implemented yet —
that begins at Milestone 1.
