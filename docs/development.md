# Development Philosophy

These principles apply to every milestone of API Monitor, not just
Milestone 0. When in doubt, favor the option that keeps this list true.

See [Useful commands](#useful-commands) at the end of this document for
what actually works in the repository today.

## 1. Understand before implementing

Every major component should have a documented reason for existing before
it is written. If you can't explain in a sentence why a package/type/
interface needs to exist right now, it probably shouldn't yet. See
[architecture.md](architecture.md) for the current component
justifications.

## 2. Small milestones

Each milestone should produce something that actually builds, runs, and can
be verified — not a partial slice of several features at once. Prefer
finishing one component's happy path over starting five components.

## 3. Standard library first

Avoid third-party dependencies unless they solve a meaningful, specific
problem the standard library doesn't. Milestones 0–5 have zero non-stdlib
dependencies by design. M6 introduces the first ones — `pgx` (PostgreSQL
has no standard-library driver at all) and `golang-migrate` (a real
migration tool, not a hand-rolled one — see `docs/architecture.md`) — with
the dependency surface kept deliberately small (verified via `go list
-deps`, not just assumed). Every dependency is a thing that can break,
need upgrading, or carry a vulnerability.

## 4. Clear separation of responsibilities

No giant `main.go`. `main.go`'s job is to wire components together and
start/stop the process — it should not contain business logic. Business
logic lives in `internal/` packages, each with one responsibility (see
architecture.md).

## 5. Production mindset

Even early code should be written with these questions in mind, because
retrofitting them later is expensive:

- **Failure** — what happens when a dependency errors? Is it handled or
  does it silently pass?
- **Timeouts** — does every network call have a bounded timeout? (An
  API monitor that hangs on an unreachable target defeats its own purpose.)
- **Concurrency** — is shared state protected? Is it safe for many targets
  to be checked at once?
- **Graceful shutdown** — can the process stop cleanly on SIGINT/SIGTERM
  without losing in-flight work or leaving connections dangling?
- **Observability** — can we tell what the service is doing from its logs?
- **Scalability** — does a design choice block scaling later, even if we
  don't scale now?
- **Reliability** — does a transient failure (one bad check) cascade into
  a bigger failure?
- **Testing** — can this be tested without a live network/database?

## 6. Don't over-engineer

We are designing so the system *can* scale into a distributed
scheduler/worker/queue architecture later (see architecture.md's "Future
scaling direction"), but we do not build that infrastructure now. Concretely,
this means: keep interfaces where a second implementation is genuinely
expected soon, avoid interfaces where there is only ever one implementation
in sight, and don't add configuration knobs, queues, or abstraction layers
for scenarios the project hasn't reached yet.

As of M6, PostgreSQL is a real, justified dependency (see
`docs/architecture.md`) and `docker-compose.yml` now exists because there
is finally something to orchestrate (api-monitor + postgres). No Kafka,
Redis, Kubernetes, or observability stack, because nothing in the current
architecture creates a problem those would solve. See
`docs/roadmap.md` for when each is expected to become justified.

## Local development

Two ways to run the project locally — pick based on what you're doing:

### Native Go (fastest for day-to-day coding)

```bash
go run ./cmd/api-monitor
```

Requires a PostgreSQL instance reachable at `DATABASE_URL` (defaults to
`postgres://postgres:postgres@localhost:5432/apimonitor` — a local
Postgres with a `postgres`/`postgres` superuser and an `apimonitor`
database already created). This is the fastest inner loop: no image
build, no container startup, just `go run`/`go build`. Best when you're
iterating on Go code and already have Postgres running locally (installed
directly, or via `docker run postgres:16.4` on its own).

### Docker Compose (full-stack, closest to how it actually deploys)

```bash
cp .env.example .env   # then edit passwords if you want non-default ones
docker compose up --build
```

Builds the application image and starts both `api-monitor` and
`postgres`, wired together exactly as `docker-compose.yml` describes (see
`docs/architecture.md`). Best when you want to verify the whole stack
together — the actual Docker image, real container networking, the
Postgres healthcheck/`depends_on` startup ordering — not just the Go code
in isolation. Slower than `go run` per iteration (image rebuild each
time), so it's not the default inner loop for active coding.

```bash
docker compose down      # stop containers; postgres_data volume survives
docker compose down -v   # stop containers AND delete the volume — this
                          # destroys all PostgreSQL data; only do this
                          # deliberately
```

## Testing

Two distinct kinds of tests exist, deliberately kept separate:

- **Unit tests** (`internal/target`, `internal/checker`, `internal/scheduler`,
  `internal/worker`, `internal/api`, `internal/storage` — the in-memory
  repository, `internal/config`) require no external infrastructure at
  all. `go test ./...` runs and passes all of these with nothing else
  running.
- **PostgreSQL integration tests** (`internal/storage/postgres`) exercise
  `TargetRepository` and a full REST-to-database round trip against a
  real PostgreSQL instance — never a mock of SQL, which wouldn't actually
  prove the queries or schema are correct. They automatically **skip**
  (not fail) if no PostgreSQL is reachable, so `go test ./...` still
  passes cleanly on a machine with no PostgreSQL set up:

  ```bash
  go test ./...   # unit tests run for real; postgres tests skip cleanly
                   # if no database is reachable, run for real if one is
  ```

  To run them for real, point `TEST_DATABASE_URL` at a **dedicated test
  database** (never your normal development database — these tests
  `TRUNCATE` it before every test):

  ```bash
  # with the local PostgreSQL used for native `go run` above:
  createdb apimonitor_test   # once, if it doesn't already exist
  go test ./internal/storage/postgres/...
  # TEST_DATABASE_URL defaults to
  # postgres://postgres:postgres@localhost:5432/apimonitor_test?sslmode=disable
  # override it if your local setup differs:
  TEST_DATABASE_URL=postgres://user:pass@host:5432/apimonitor_test?sslmode=disable \
    go test ./internal/storage/postgres/...
  ```

  Set `REQUIRE_POSTGRES_TESTS=1` to turn "no database reachable" into a
  hard failure instead of a skip — useful in CI, where a missing test
  database should be caught, not silently passed over. Set
  `SKIP_POSTGRES_TESTS=1` to always skip them regardless of whether a
  database is reachable.

## Useful commands

Run from the repository root. All of these are verified to work as of M6
(the `docker compose`/`docker build` commands were reviewed carefully but
not executed — no Docker daemon in the environment these docs were
written in; verify them yourself before relying on them):

```bash
gofmt -w .          # format
go vet ./...         # static analysis
go build ./...        # build everything
go test ./...          # run all tests (postgres integration tests skip
                        # cleanly without a database — see Testing above)
go test -race ./...     # run all tests with the race detector
docker build -t api-monitor .   # build the container image alone
docker compose config            # validate docker-compose.yml
docker compose up --build         # run the full stack (api-monitor + postgres)
```
