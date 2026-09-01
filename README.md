# InfraVex API Monitor

Continuous HTTP/HTTPS API monitoring — reachability, latency, status
correctness, uptime, and incident detection.

**Status: under active development (Milestone 2). Target management has a
working REST API; actual API monitoring (checks, scheduling, incidents,
alerts) is not implemented yet.**

## Problem statement

Services fail silently from the inside — a process can stay "up" while the
API it serves times out, returns errors, or is simply unreachable. API
Monitor exists to catch that from the outside, the way a real client would
experience it: by making real requests on a schedule and tracking whether
they succeed, how fast they are, and when that changes.

## What this project will do

Once the core milestones are complete, API Monitor will:

- Continuously check registered HTTP/HTTPS targets on a schedule
- Measure latency and detect timeouts/connection failures
- Verify responses against an expected status code
- Track UP/DOWN state per target and derive uptime history
- Detect incidents (state transitions) and their recovery
- Send alerts (starting with webhooks) when a target goes down or recovers
- Expose a REST API to manage targets and query results/incidents

None of this is implemented yet — see [Current status](#current-status).

## Current status

The domain model (`Target`, `CheckResult`, `Incident`) is implemented
(M1), and target management now has a working REST API backed by
in-memory storage (M2): create, list, get, update, and delete targets over
HTTP, plus a `/health` liveness endpoint. See
[docs/api.md](docs/api.md) for the full API reference.

No HTTP checking, scheduling, incident detection, alerting, or durable
persistence exists yet — the server accepts target configuration but does
not act on it.

## High-level architecture

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

API Monitor starts as a **modular monolith**: one deployable binary made of
independently-responsible internal packages. This keeps the system simple
to build and run now, while keeping the door open to later splitting the
scheduler/checker into distributed workers behind a queue, without a
rewrite. Full details, including data flow, are in
[docs/architecture.md](docs/architecture.md).

## Planned features

See [docs/roadmap.md](docs/roadmap.md) for the full 12-milestone plan, from
domain modeling through persistence, alerting, a REST API, observability,
and eventual distributed scaling.

## Technology choices

- **Language:** Go (currently built/tested with Go 1.25.1)
- **Dependencies:** Go standard library only, for now. Third-party packages
  are added only when they solve a specific problem the stdlib doesn't
  (e.g., a PostgreSQL driver, planned for M6).
- **Persistence:** PostgreSQL (planned, M6) — not yet implemented.

## Development roadmap

| Milestone | Focus |
|---|---|
| M0 | Foundation & architecture *(done)* |
| M1 | Domain model *(done)* |
| M2 | Target management + REST backend + Docker foundation *(current)* |
| M3 | HTTP checker |
| M4 | Scheduler |
| M5 | Concurrent workers |
| M6 | Persistence (PostgreSQL) + Docker Compose |
| M7 | Health & incident engine |
| M8 | Alerting |
| M9 | Kafka / event-driven architecture |
| M10 | Observability |
| M11 | Productionization |
| M12 | Scaling & hardening |

Details in [docs/roadmap.md](docs/roadmap.md).

## How to run the current project

```bash
go run ./cmd/api-monitor
```

This starts an HTTP server on `:8080` (configurable — see
[docs/api.md](docs/api.md) and `docs/development.md`). Try it:

```bash
curl http://localhost:8080/health

curl -X POST http://localhost:8080/api/v1/targets \
  -d '{"name":"Example API","url":"https://example.com/health","method":"GET","interval":"30s","timeout":"5s","expected_status_code":200}'

curl http://localhost:8080/api/v1/targets
```

Press `Ctrl+C` to stop it — shutdown is graceful. Data is in-memory only
and is lost on restart (durable storage arrives in M6).

A `Dockerfile` is also provided: `docker build -t api-monitor .` (not yet
verified in this environment — see `CLAUDE.md`).

## Project philosophy

- Understand a component's purpose before implementing it.
- Ship in small, working milestones.
- Prefer the standard library; add dependencies only for specific,
  justified problems.
- Keep responsibilities separated — no giant `main.go`.
- Design early with failure, timeouts, concurrency, and graceful shutdown
  in mind.
- Design for future scale without prematurely building distributed
  infrastructure.

Full write-up in [docs/development.md](docs/development.md).

## License

[MIT](LICENSE)
