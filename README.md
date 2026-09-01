# InfraVex API Monitor

Continuous HTTP/HTTPS API monitoring — reachability, latency, status
correctness, uptime, and incident detection.

**Status: under active development (Milestone 6). The application now
performs real, periodic, concurrency-bounded HTTP checks against
registered targets, and durably persists those targets in PostgreSQL —
they survive an application restart. Incident detection and alerting are
not implemented yet.**

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
(M1). Target management has a working REST API (M2): create, list, get,
update, and delete targets over HTTP, plus a `/health` liveness endpoint
— see [docs/api.md](docs/api.md).

The application actually monitors what you register: a scheduler (M4)
periodically triggers a real HTTP/HTTPS check (M3) for every enabled
target on its own configured interval, and a bounded worker pool (M5)
caps how many checks run concurrently so registering many targets can't
exhaust goroutines or connections.

As of M6, targets are **durably persisted in PostgreSQL** — create one,
restart the process, and it's still there. `CheckResult`s themselves
aren't persisted or interpreted yet — no incident detection or alerting
exists — a check's outcome currently only reaches an internal log line.

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
- **Dependencies:** Go standard library first. Third-party packages are
  added only when they solve a specific problem the stdlib doesn't — as
  of M6, that's `pgx` (PostgreSQL has no standard-library driver at all)
  and `golang-migrate` (schema migrations). The dependency surface is
  kept deliberately small; see `docs/architecture.md`.
- **Persistence:** PostgreSQL (M6), via `pgx` used natively (not through
  `database/sql`). Schema migrations are embedded in the binary and run
  automatically at startup.

## Development roadmap

| Milestone | Focus |
|---|---|
| M0 | Foundation & architecture *(done)* |
| M1 | Domain model *(done)* |
| M2 | Target management + REST backend + Docker foundation *(done)* |
| M3 | HTTP checker *(done)* |
| M4 | Scheduler *(done)* |
| M5 | Worker pool + concurrent check execution *(done)* |
| M6 | PostgreSQL persistence + Docker Compose *(current)* |
| M7 | Incident engine |
| M8 | Alerting |
| M9 | Kafka / event-driven architecture |
| M10 | Observability |
| M11 | Productionization |
| M12 | Scaling & hardening |

Details in [docs/roadmap.md](docs/roadmap.md).

## How to run the current project

Requires a reachable PostgreSQL instance (see `DATABASE_URL` in
`docs/development.md`; defaults to `postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable`
for zero-config local development). Schema migrations run automatically
at startup.

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

Once created, the target is checked automatically on its own configured
`interval` — watch the logs for `"check completed"` lines. Concurrency is
bounded by `WORKER_COUNT` (default 10) and `QUEUE_SIZE` (default 100),
both configurable via environment variables.

Press `Ctrl+C` to stop it — shutdown is graceful. Data is durably
persisted in PostgreSQL — restart the process and it's still there.

Or run the full stack (api-monitor + PostgreSQL) with Docker Compose —
see `docs/development.md`:

```bash
cp .env.example .env
docker compose up --build
```

(The `docker compose`/`docker build` commands were reviewed carefully but
not executed in this environment — no Docker daemon available; see
`CLAUDE.md`.)

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
