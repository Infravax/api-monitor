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
Current Milestone: M6 (complete)
```

## Completed Milestones

```
M0 — Foundation & Architecture
M1 — Domain Model
M2 — Target Management + REST Backend + Docker Foundation
M3 — HTTP Checker
M4 — Scheduler
M5 — Worker Pool + Concurrent Check Execution
M6 — PostgreSQL Persistence + Docker Compose
```

## Current Architecture

Modular monolith. Full design in `docs/architecture.md` — see "PostgreSQL
Persistence (Milestone 6)" for what M6 added.

**What's actually running in `cmd/api-monitor/main.go` as of M6**:
```
postgres.NewPool(ctx, cfg.DatabaseURL)     (fails fast, non-zero exit, if
                                             unreachable within 10s)
postgres.Migrate(cfg.DatabaseURL)          (embedded SQL, run every startup,
                                             ErrNoChange treated as success)

net/http server → RequestID/Logging/Recovery middleware → TargetHandler
       → target.Service → Target.Validate → target.Repository
       → postgres.TargetRepository → PostgreSQL (targets table)

worker.Pool.Start(ctx)        (WorkerCount goroutines, bounded queue —
                                unchanged since M5, never touches the DB)

scheduler.Scheduler.Start(ctx)
       → (every DiscoveryInterval) target.Service.List()  ← now reaches
                                     PostgreSQL through the repository
       → per enabled target, on its own Interval:
              worker.Pool.Check(ctx, target)   (unchanged since M5)
                     → Submit: enqueue, block for a free worker,
                       wait for result
                     → pool worker: checker.Checker.Check(ctx, target)
                       (panic-recovered)
                     → CheckResult delivered back to the Scheduler's
                       per-target goroutine
              → OnResult callback (unset — CheckResults still aren't
                persisted; that needs its own table, not built in M6)

shared shutdown context (signal.NotifyContext): HTTP server → Scheduler →
worker pool → postgres pool.Close() (last, since target.Service is the
only DB consumer and both its callers are already stopped by then)
```

**`internal/scheduler`, `internal/api`, `internal/target`, and
`internal/worker` all have zero code changes for M6.**
`postgres.TargetRepository` satisfies `target.Repository` exactly — the
same interface `storage.MemoryTargetRepository` (M2) already satisfied —
so only `main.go`'s wiring and `internal/config` changed. This is the
same interface-boundary pattern that made M5's worker pool a clean swap.

## Domain Model

Unchanged since M3. See `docs/architecture.md`'s "Domain model (Milestone
1)" and "The HTTP Checker (Milestone 3)" sections. The PostgreSQL schema
(below) mirrors `target.Target` field-for-field — no columns were invented
that don't exist in the Go domain type.

## M6 Implementation

- **Dependencies added**: `github.com/jackc/pgx/v5` (PostgreSQL driver,
  used natively via `pgxpool` — not through `database/sql`; there is no
  standard-library PostgreSQL driver, so this is the first genuinely
  justified third-party dependency in the project), `github.com/jackc/pgerrcode`
  (named SQLSTATE constants for error mapping — already a transitive
  dependency of `golang-migrate`'s own pgx driver, so this adds no new
  dependency surface), `github.com/golang-migrate/migrate/v4` (a real,
  widely-used migration tool, not a hand-rolled one). Kept minimal:
  no ORM, no second PostgreSQL driver.
- **`internal/storage/postgres` (new package)** — owns everything
  PostgreSQL-specific. `internal/storage` (parent package,
  `MemoryTargetRepository`) is unaware this subpackage exists.
  - `pool.go`: `NewPool(ctx, databaseURL) (*pgxpool.Pool, error)` — builds
    a `pgxpool.Pool` (`MaxConns = 10`, an explicit constant, not exposed
    as an env var — no measured need to tune it yet) and `Ping`s it with a
    5s timeout before returning, so a bad `DATABASE_URL` or unreachable
    host fails loudly at startup. `Migrate(databaseURL) error` — runs
    embedded migrations (`//go:embed migrations/*.sql`) via
    `golang-migrate`, through a separate short-lived `database/sql`
    connection (`pgx/v5/stdlib`) distinct from the app's long-lived
    `pgxpool.Pool`; `migrate.ErrNoChange` is treated as success, so it's
    safe to call on every startup.
  - `repository.go`: `TargetRepository` implements
    `Create/Get/List/Update/Delete` against a `targets` table.
    `List` uses `ORDER BY name, id` explicitly — the same ordering
    `MemoryTargetRepository` already used — never relying on incidental
    PostgreSQL row order. `Update`/`Delete` check `RowsAffected() == 0` to
    detect a missing row rather than a separate existence check.
    `mapError` translates `pgx.ErrNoRows` → `target.ErrNotFound` and a
    unique-violation (SQLSTATE `23505` via `pgerrcode`) →
    `target.ErrAlreadyExists`, so `target.Service` never needs to know
    PostgreSQL is involved.
  - `migrations/000001_create_targets_table.{up,down}.sql`: one table,
    `id UUID PRIMARY KEY`, `name/url/method TEXT NOT NULL`,
    `interval_ns/timeout_ns BIGINT NOT NULL CHECK (> 0)` (raw
    nanoseconds — `time.Duration`'s own unit, zero conversion),
    `expected_status_code INTEGER NOT NULL CHECK (BETWEEN 100 AND 599)`,
    `enabled BOOLEAN NOT NULL DEFAULT true`. No `created_at`/`updated_at`
    — the Go domain type has neither. No `UNIQUE` on name/url — nothing in
    the domain model requires that. No index beyond the implicit
    primary-key one — a full scan+sort is fine at this scale.
- **No transactions** — every repository method is one SQL statement;
  nothing in M6 needs multiple writes to succeed or fail atomically
  together.
- **How the Scheduler gets persisted targets on startup**: no special
  loading step was added. `scheduler.Scheduler` already re-lists targets
  via `TargetLister.List()` once immediately on `Start` and every
  `DiscoveryInterval` after (M4's design) — so the first reconciliation
  pass after a restart naturally picks up whatever PostgreSQL has. Same
  "later milestone's requirement already satisfied by an earlier
  milestone's design" story as M5.
- **`storage.MemoryTargetRepository` was kept, not deleted.** It still
  satisfies `target.Repository` and remains useful for tests that don't
  want a database dependency (e.g. `internal/api` handler tests). Only
  `main.go`'s production wiring changed to use PostgreSQL.
- **`internal/config`**: added `DatabaseURL string` (env `DATABASE_URL`,
  defaults to `postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable`
  for zero-config `go run`). `Config.LogValue()` (used so `main.go` can
  log the whole config in one call) redacts the password via
  `redactDatabaseURL` before logging — never logs a real credential.
- **`docker-compose.yml` (new)**: `postgres` (pinned `16.4`, not
  `latest`; named volume `postgres_data`; `pg_isready` healthcheck) and
  `api-monitor` (`depends_on: condition: service_healthy`, so Compose's
  own dependency mechanism — not a hand-rolled wait script — ensures the
  app never tries to connect before PostgreSQL is actually ready).
  `docker compose down` preserves the volume; only `down -v` destroys it.
- **`.env.example` (new)**: placeholder `POSTGRES_DB`/`POSTGRES_USER`/
  `POSTGRES_PASSWORD`/`DATABASE_URL` values only. `.env` was already
  gitignored (`.gitignore`'s existing `.env`/`.env.*` rules, unchanged).
- **`Dockerfile`**: added `ca-certificates` (a latent gap since M3 — the
  Checker makes real outbound HTTPS requests and needs a trusted root
  store; Alpine doesn't include one by default — fixed while already
  touching this file for M6, not something M6 itself required). No other
  changes needed; `DATABASE_URL` flows through the same env-var mechanism
  every other config value already used.
- **`/health` is unchanged** — still pure liveness, deliberately not made
  PostgreSQL-aware. A liveness/readiness split is deferred to M10;
  container startup ordering is already solved by Compose's
  `depends_on: condition: service_healthy`.
- **Not built in M6, on purpose**: persisting `CheckResult`s (the
  `OnResult` callback from M4/M5 stays unset — needs its own table, not a
  repurposing of `TargetRepository`), Kafka, an ORM, connection-pool
  tuning exposed as config, and any change to `target.Repository`'s
  interface itself (M6 was a pure implementation swap, same as M5).

## Configuration

New environment variable (`internal/config/config.go`), same
silent-fallback-to-default philosophy as every other setting:
```
DATABASE_URL=postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable
```
(`WORKER_COUNT`/`QUEUE_SIZE` from M5 are unchanged.)

## Important Decisions

- **Why `pgx` over `database/sql` + `lib/pq`**: PostgreSQL has no
  standard-library driver at all, so unlike every prior dependency
  decision, a third-party one is actually justified here. `pgx` is the
  actively-maintained, de facto standard (`lib/pq` is in maintenance
  mode), has first-class `context.Context` support, and its own
  connection pool (`pgxpool`) — going native avoids an abstraction layer
  (`database/sql`) with no current purpose, since nothing in this project
  needs to swap PostgreSQL for a different database engine.
- **Why `golang-migrate` over hand-rolled migrations**: a real,
  widely-used migration tool with `Up`/idempotent-safe semantics, per the
  M6 brief's explicit instruction not to hand-roll one.
- **Why migrations run through a separate `database/sql` connection**:
  `golang-migrate`'s database driver is built on `database/sql`;
  sharing the app's long-lived `pgxpool.Pool` with a one-time startup
  step would be pointless coupling.
- **Why no transactions**: every repository method is a single
  statement; nothing in M6 has a multi-write atomicity requirement.
- **Why no `created_at`/`updated_at`/other invented columns**: the Go
  `target.Target` domain type doesn't have them; adding columns nothing
  reads or writes would be exactly the kind of speculative addition this
  project avoids elsewhere (same reasoning as M1's original scope call).
- **Why database-level `CHECK`/`NOT NULL` but not a `method` allow-list
  constraint**: constraints protect structural invariants (defense in
  depth against buggy/direct writes); business rules like "which HTTP
  methods are valid" stay solely `Target.Validate`'s (M1) job, so there's
  one source of truth, not two.
- **Why the connection pool's `MaxConns` isn't an env var yet**: current
  database load (REST CRUD + the Scheduler's periodic `List()`) is
  low-volume — the worker pool and checker never touch the database.
  Adding a tuning knob nobody has a reason to turn yet would be
  speculative configuration (`docs/development.md` principle 6).
- **Why `MemoryTargetRepository` was kept, not deleted**: it remains
  genuinely useful for tests that shouldn't need a live database (e.g.
  `internal/api` handler tests) — the same "keep an interface's simpler
  implementation around for testing" reasoning that applies throughout
  this codebase.
- **Why Kafka is still postponed**: unchanged from M5's reasoning —
  nothing in the current architecture produces events that need a
  durable, external broker yet. M9 revisits this once `CheckResult`s need
  to flow to multiple independent consumers.
- **Why `internal/scheduler`/`internal/api`/`internal/target`/
  `internal/worker` have zero code changes**: `postgres.TargetRepository`
  implements the exact `target.Repository` interface shape
  `target.Service` already depended on since M2 — validating that M2's
  interface boundary was drawn in the right place, the same story M5
  already told about `scheduler.TargetChecker`.

## Testing

Run from repo root — all actually executed for M6 (against a real local
PostgreSQL instance reachable at the default `DATABASE_URL`, databases
`apimonitor` and `apimonitor_test` both present):
```
gofmt -w .                # ok, no changes needed in M6-touched files
go vet ./...                # ok
go build ./...                # ok
go test ./...                    # ok — all packages pass, including 13
                                  #      postgres integration tests run
                                  #      for real (not skipped)
go test -race ./...                # ok — no data races
```
Key `internal/storage/postgres` tests (all run against a real database,
never mocked — a mocked SQL layer couldn't prove the schema or queries
are correct): `TestTargetRepository_CreateAndGet`,
`TestTargetRepository_CreateDuplicate` (→ `target.ErrAlreadyExists`),
`TestTargetRepository_GetMissing`/`UpdateMissing`/`DeleteMissing` (→
`target.ErrNotFound`), `TestTargetRepository_List` (proves `ORDER BY
name, id`), `TestTargetRepository_ContextCancellation`,
`TestMigrate_IsIdempotent`, `TestNewPool_FailsFastOnUnreachableHost`.
`e2e_test.go` additionally proves the full stack:
`TestE2E_RESTCrudFlow_WithPostgres` (Handler → Service → PostgreSQL
Repository → real HTTP round trip through every M2 REST endpoint) and
**`TestE2E_DataSurvivesRestart`** — the core M6 acceptance test: create a
target, close every in-process object (server, service, repository,
pool), rebuild the entire stack from scratch against the same database,
confirm the target is still there.

Tests skip cleanly (not fail) when no PostgreSQL is reachable
(`requireDatabase` in `repository_test.go`), so `go test ./...` still
passes on a machine with no database configured; `REQUIRE_POSTGRES_TESTS=1`
turns that skip into a hard failure (for CI); `SKIP_POSTGRES_TESTS=1`
always skips regardless.

**Manually verified end-to-end, beyond the automated tests** (built the
binary, ran it as a real OS process against the local PostgreSQL
instance, not just `go test`):
- **Persistence across restart**: started the app, `POST`ed a target,
  sent `SIGTERM`, confirmed clean shutdown logging, started the app again
  from the same binary, `GET` the same target ID — returned the same
  target unchanged. Deleted it afterward to leave the database as found.
- **Fail-fast on unreachable database**: pointed `DATABASE_URL` at a
  closed port; the process logged `"failed to connect to postgresql"`
  and exited with status 1 within the connect timeout — no hang, no
  silent partial start.
- **Graceful shutdown order**: `SIGTERM` produced, in order,
  `"http server stopped cleanly"` → `"scheduler stopped cleanly"` →
  `"worker pool stopped cleanly"` → `"database connection pool closed"`
  — matching `main.go`'s explicit sequencing (server, then scheduler,
  then pool, then the database pool closed last since `target.Service`
  is its only consumer and both callers are already stopped by then).
- **Scheduler discovery of persisted targets**: a target created in an
  earlier run was picked up and scheduled (`"target scheduled"` logged)
  immediately on the next process start, with no separate load step.

**Docker itself is not available in this environment** (no `docker`
binary installed at all — same underlying limitation noted for
M2/M3/M4/M5, one step further here: even the CLI, not just a daemon, is
absent). `docker-compose.yml` was written and reviewed carefully
(service definitions, healthcheck, `depends_on`, named volume) but
**`docker compose config`/`up`/`down` were never executed** — this
remains genuinely unverified, not claimed as passing. A real PostgreSQL
instance (installed directly via the OS package manager, running as a
systemd service) was used instead for all integration/e2e/manual testing
above, which verifies the application and schema logic but not the
Compose file's own YAML correctness or container networking.

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go              (wiring only — now also connects
│                                          to PostgreSQL and runs migrations
│                                          before constructing the repository)
├── internal/
│   ├── target/    target.go, repository.go, service.go, doc.go + tests
│   ├── checker/   check_result.go, check.go, doc.go + tests
│   ├── scheduler/ scheduler.go, doc.go + tests                (unchanged since M5)
│   ├── worker/    pool.go, doc.go + tests + benchmark          (unchanged since M5)
│   ├── incident/  incident.go, doc.go + tests                (M1, still unused)
│   ├── id/        id.go                                      (shared UUID util)
│   ├── config/    config.go (+DatabaseURL) + tests
│   ├── storage/   memory_target_repository.go, doc.go + tests
│   │   └── postgres/  pool.go, repository.go, doc.go (NEW M6)
│   │       ├── repository_test.go, e2e_test.go (NEW M6 — integration
│   │       │   tests against a real PostgreSQL instance)
│   │       └── migrations/000001_create_targets_table.{up,down}.sql (NEW M6)
│   ├── api/       server.go, router.go, target_handler.go,
│   │              health_handler.go, middleware.go, response.go,
│   │              target_dto.go, doc.go + tests
│   └── alert/     doc.go only (not implemented)
├── docs/architecture.md, development.md, roadmap.md, api.md
├── Dockerfile, .dockerignore, docker-compose.yml (NEW M6), .env.example (NEW M6)
├── README.md, LICENSE, .gitignore, go.mod, go.sum, CLAUDE.md
```

## Next Milestone

**M7 — Incident Engine** is next: apply failure/recovery thresholds to
the stream of `CheckResult`s the worker pool already produces (currently
reaching only an unset `OnResult` callback and a log line) to derive
UP/DOWN state per target, open/resolve `Incident` records (the M1 domain
type — defined but still completely unused), and persist them. This is
the first milestone that will need `CheckResult` persistence (its own
table/repository, not a repurposing of `TargetRepository`) and will
likely be where `OnResult` finally gets wired to something real. Do not
implement Kafka (M9) as part of this — M7's brief is thresholds and state
transitions, not event-driven architecture.
