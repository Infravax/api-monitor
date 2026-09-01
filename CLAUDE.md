# CLAUDE.md

Working memory for future Claude Code sessions on this project. Update this
file at the end of every milestone. It must describe the actual state of
the repository — not planned or aspirational functionality.

## Project Identity

```
Project: InfraVex API Monitor
Organization: InfraVex
Language: Go
Module: github.com/InfraVex/api-monitor
Current Milestone: M2 (complete)
```

## Current Architecture

Modular monolith. One binary (`cmd/api-monitor`), internal packages each
owning one responsibility. Full design in `docs/architecture.md` (see its
"Request architecture (Milestone 2)" section for what's actually wired up
today vs. the target design).

As of M2, a real HTTP path exists end to end:
`net/http server → RequestID/Logging/Recovery middleware → TargetHandler →
target.Service → Target.Validate → target.Repository →
storage.MemoryTargetRepository`. `checker`, `scheduler`, `incident`,
`alert` remain doc-only (M1's domain types in `checker`/`incident` are
implemented, but nothing consumes them yet).

## Completed Milestones

```
M0 — Foundation & Architecture
M1 — Domain Model
M2 — Target Management + REST Backend + Docker Foundation
```

## Domain Model

Unchanged from M1 — see `docs/architecture.md`'s "Domain model (Milestone
1)" section for full detail. Summary:

- **`Target`** (`internal/target`) — endpoint + check policy
  (name/URL/method/interval/timeout/expected status/enabled). Validated by
  `Target.Validate()`; constructed via `target.New(NewParams)`.
- **`CheckResult`** (`internal/checker`) — outcome of one check attempt,
  via a stored `Outcome` enum, not a raw `Success` bool.
- **`Incident`** (`internal/incident`) — open/resolved period, derived
  from `ResolvedAt == nil`, not a separate status field.

## M2 Implementation

- **HTTP server** (`internal/api/server.go`) — `net/http.Server` only, no
  framework. Configurable `Addr`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout`.
- **Router** (`internal/api/router.go`) — Go 1.22+ `http.ServeMux` with
  method + `{id}` path-parameter patterns; no third-party router needed.
- **Routes**:
  - `GET /health` — liveness only (see `docs/api.md`)
  - `POST /api/v1/targets`, `GET /api/v1/targets`,
    `GET /api/v1/targets/{id}`, `PUT /api/v1/targets/{id}`,
    `DELETE /api/v1/targets/{id}`
- **Handler** (`internal/api/target_handler.go`) — parses/validates at the
  transport boundary (JSON decode, duration parsing, 1 MiB body cap via
  `http.MaxBytesReader`), maps domain errors to HTTP status/JSON via
  `writeServiceError`, contains no business logic.
- **DTOs** (`internal/api/target_dto.go`) — `targetRequest`/
  `targetResponse`, separate from `target.Target` for two concrete
  reasons: (1) `time.Duration` has no built-in human-friendly JSON
  encoding (`"30s"` on the wire vs. int64 nanoseconds internally), and (2)
  it decouples the wire contract from the domain type's shape.
- **Service** (`internal/target/service.go`) — `Service.{Create,Get,List,
  Update,Delete}`, knows nothing about HTTP. `Update` fetches the existing
  target, rebuilds it with `Target.Validate()` (not `New`, so the ID is
  preserved), then persists.
- **Repository interface** (`internal/target/repository.go`) —
  `Repository` defined in `internal/target` (the consumer/owner of
  `Target`), not in `internal/storage`. `ErrNotFound`/`ErrAlreadyExists`
  sentinels.
- **In-memory storage** (`internal/storage/memory_target_repository.go`) —
  `MemoryTargetRepository`, `map[string]target.Target` behind a
  `sync.RWMutex`. `List` sorts by Name then ID for deterministic output
  (the domain model has no creation timestamp to sort by). Concurrency
  verified with `go test -race`.
- **Configuration** (`internal/config/config.go`) — env vars with
  defaults, no framework: `HTTP_ADDR` (`:8080`), `HTTP_READ_TIMEOUT`
  (`5s`), `HTTP_WRITE_TIMEOUT` (`10s`), `HTTP_IDLE_TIMEOUT` (`60s`).
  Unparsable values silently fall back to the default.
- **Middleware** (`internal/api/middleware.go`) — `RequestID` (reuses
  `internal/id` from M1, no separate UUID dependency) → `Logging`
  (method/path/status/duration/request_id via `log/slog`) → `Recovery`
  (panic → 500 JSON, doesn't crash the server). Order matters: `Logging`
  wraps `Recovery` so a recovered panic still gets an accurate status
  logged.
- **`main.go`** — pure wiring: loads config, builds repository → service →
  handler → server, runs `ListenAndServe` in a goroutine, waits on
  `signal.NotifyContext` or a server error, then `Shutdown` with a 10s
  timeout.
- **Docker** (`Dockerfile`, `.dockerignore`) — multi-stage:
  `golang:1.25-alpine` build stage, `alpine:3.20` runtime stage, non-root
  user, `HEALTHCHECK` via `wget` against `/health`. No `docker-compose.yml`
  — not justified yet (single service, nothing to orchestrate against
  until PostgreSQL in M6).

## Important Decisions

- **Standard library HTTP, no framework**: routing needs (method + one
  path param) are fully covered by Go 1.22+'s `http.ServeMux`; a
  framework would be a dependency with no problem left for it to solve.
- **In-memory storage now, PostgreSQL postponed to M6**: `target.Repository`
  is an interface, so this is a swap, not a rewrite, when M6 arrives —
  same pattern validated by M1's prediction that `Validate()` (not just
  `New()`) would be needed for exactly this kind of external
  reconstruction, which `Service.Update` now actually uses.
- **Kafka postponed**: nothing in the current architecture produces events
  that need a broker; M9 (event-driven check results/incidents) is where
  this becomes justified, per the roadmap re-ordering below.
- **Still a modular monolith**: one binary, clean internal seams
  (`Handler → Service → Repository`), no network hop between them. Not
  revisited in M2.
- **Roadmap re-ordering**: `docs/roadmap.md` moved the target REST API
  and the first Docker image from their original M9/M11 slots into M2
  itself, since M2 was the milestone that introduced the HTTP server they
  depend on. M9 is now "Kafka / event-driven architecture" (check
  results/incidents, building on M2's REST foundation rather than
  duplicating it); M11 is production hardening beyond the M2 Docker
  foundation. `docs/roadmap.md` documents this change and why.
- **`UpdateParams` includes `Enabled`, `NewParams` does not**: a new
  target always starts enabled (M1 decision, unchanged); an existing one
  must be pause/resume-able via `PUT`, which is a create-vs-update
  asymmetry, not an oversight.
- **`Enabled *bool` in the request DTO**: distinguishes "omitted" (defaults
  to `true`) from "explicitly `false`", for both create and update.
- **1 MiB request body cap**: cheap, standard-library-only
  (`http.MaxBytesReader`), and a real (if basic) protection against
  oversized payloads now that the server accepts arbitrary client JSON.

## Dependencies

None beyond the Go standard library. `go.mod` has no `require` block, no
`go.sum` exists. (Same as M0/M1 — still true after M2.)

## Verification

Run from repo root:
```
gofmt -w .            # ok, only reformatted whitespace
go vet ./...           # ok
go build ./...          # ok
go test ./...            # ok — all packages pass
go test -race ./...       # ok — no data races, including the concurrent
                          #      MemoryTargetRepository stress test
```
Manually verified end-to-end: built the binary, ran it, and exercised the
full CRUD flow with `curl` — `POST` create → `GET` by id → `GET` list →
`PUT` update → `DELETE` → `GET` returns 404 → unknown id returns 404 — all
correct status codes and JSON bodies. Confirmed graceful shutdown on
SIGTERM.

**Docker was not executed** — no Docker daemon was available in the
environment this milestone was built in. The `Dockerfile` was written and
reviewed carefully (multi-stage, non-root, meaningful `HEALTHCHECK`) but
`docker build`/`docker run` have not actually been run. Verify locally
before relying on it.

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go              (wiring only)
├── internal/
│   ├── target/    target.go, repository.go, service.go, doc.go + tests
│   ├── checker/   check_result.go, doc.go + tests            (M1, unused so far)
│   ├── incident/  incident.go, doc.go + tests                (M1, unused so far)
│   ├── id/        id.go                                      (shared UUID util)
│   ├── config/    config.go + tests
│   ├── storage/   memory_target_repository.go, doc.go + tests
│   ├── api/       server.go, router.go, target_handler.go,
│   │              health_handler.go, middleware.go, response.go,
│   │              target_dto.go, doc.go + tests
│   ├── scheduler/ doc.go only (not implemented)
│   └── alert/     doc.go only (not implemented)
├── docs/architecture.md, development.md, roadmap.md, api.md
├── Dockerfile, .dockerignore
├── README.md, LICENSE, .gitignore, go.mod, CLAUDE.md
```

## Next Milestone

**M3 — HTTP Checker** is next: perform real HTTP/HTTPS requests against a
`Target` and produce a `checker.CheckResult` (the M1 type, unused until
now). Expect this to be the first place `checker.CheckResult`'s
`Outcome` enum actually gets set by real logic (success vs.
unexpected_status vs. timeout vs. connection_error), and the first
component that needs a bounded per-request timeout distinct from the HTTP
server's own timeouts.
