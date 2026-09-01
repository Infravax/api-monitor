# InfraVex API Monitor

Continuous HTTP/HTTPS API monitoring — reachability, latency, status
correctness, uptime, and incident detection.

**Status: under active development (Milestone 0 — foundation only). No
monitoring functionality is implemented yet.**

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

This repository currently contains only the Milestone 0 foundation:
project structure, Go module setup, documentation, and empty package
skeletons for future components. The binary builds and starts, but performs
no monitoring.

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
| M0 | Foundation & architecture *(current)* |
| M1 | Domain model |
| M2 | Target management |
| M3 | HTTP checker |
| M4 | Scheduler |
| M5 | Concurrent workers |
| M6 | Persistence |
| M7 | Health & incident engine |
| M8 | Alerting |
| M9 | REST API |
| M10 | Observability |
| M11 | Productionization |
| M12 | Scaling & hardening |

Details in [docs/roadmap.md](docs/roadmap.md).

## How to run the current project

At this stage, the binary only starts up, logs a message, and exits
cleanly on interrupt/termination — there is nothing to monitor yet.

```bash
go run ./cmd/api-monitor
```

Press `Ctrl+C` to stop it.

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
