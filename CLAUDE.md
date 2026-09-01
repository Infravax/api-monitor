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
Current Milestone: M1 (complete)
```

## Current Architecture

Modular monolith. One binary (`cmd/api-monitor`), internal packages each
owning one responsibility (`target`, `checker`, `scheduler`, `incident`,
`alert`, `storage`, `api`). Full design in `docs/architecture.md`. As of
M1, only the domain types inside `target`, `checker`, and `incident` are
implemented — everything else is still an empty, documented package
(`doc.go` only).

## Completed Work

**M0** — Repo scaffolding, Go module init, `internal/` package skeletons
(doc-comment only), `cmd/api-monitor/main.go` (starts, logs, graceful
shutdown on SIGINT/SIGTERM, no business logic), `docs/architecture.md`,
`docs/development.md`, `docs/roadmap.md`, `README.md`, `LICENSE`,
`.gitignore`.

**M1** — Renamed the project from `InfraCex` to `InfraVex` throughout
(module path, `main.go`, `README.md`, `docs/architecture.md`, `LICENSE`).
Implemented the domain model:
- `internal/id` — shared UUIDv4 ID generator (`id.New() string`), stdlib
  `crypto/rand` only.
- `internal/target` — `Target` type + `New`/`Validate`.
- `internal/checker` — `CheckResult` type + `Outcome` enum +
  `New`/`Validate`/`Success`.
- `internal/incident` — `Incident` type + `New`/`Validate`/`Resolve`/
  `IsOpen`/`IsResolved`/`Duration`.

All three domain packages have unit tests covering valid construction,
each documented invariant, and (for `incident`) the `Resolve` transition.

## Domain Model

### Target (`internal/target`)
```go
type Target struct {
    ID, Name, URL, Method string
    Interval, Timeout     time.Duration
    ExpectedStatusCode    int
    Enabled                bool
}
```
- `target.New(target.NewParams{...})` — defaults `Method` to `GET` and
  `ExpectedStatusCode` to `200` if left zero-valued; `Interval`/`Timeout`
  have **no default** (must be set explicitly — silently defaulting a
  check interval was judged too risky).
- Invariants (see `ErrXxx` sentinels in `target.go`): non-empty
  ID/Name/URL/Method; URL must parse with a non-empty host
  (`ErrInvalidURL`) and scheme `http`/`https` (`ErrUnsupportedScheme`);
  Method must be one of GET/POST/PUT/PATCH/DELETE/HEAD/OPTIONS; Interval
  and Timeout must be `> 0`; ExpectedStatusCode must be 100–599.
- No `CreatedAt`/`UpdatedAt` yet — nothing sets them until persistence
  (M6) or Target Management (M2) exists; adding them now would be dead
  fields.

### CheckResult (`internal/checker`)
```go
type CheckResult struct {
    ID, TargetID, ErrorMessage string
    Timestamp                  time.Time
    Outcome                    Outcome
    StatusCode                 int
    Latency                    time.Duration
}
```
- `Outcome` is one of `success`, `unexpected_status`, `timeout`,
  `connection_error`. **`Success` is not a stored field** — `Success()` is
  derived from `Outcome` so the type cannot represent a contradictory
  state (e.g. "success" with an error message).
- `StatusCode == 0` means "no HTTP response" (sentinel, since 0 is never a
  real status code) — required non-zero (100–599) for `success`/
  `unexpected_status`, must be 0 for `timeout`/`connection_error`.
- `ErrorMessage` required non-empty for `timeout`/`connection_error`, must
  be empty for `success`.
- `checker.New(checker.NewParams{...})` normalizes `Timestamp` to UTC.

### Incident (`internal/incident`)
```go
type Incident struct {
    ID, TargetID, Reason string
    StartedAt            time.Time
    ResolvedAt            *time.Time // nil = open
}
```
- No `Status` field — open/resolved is derived solely from whether
  `ResolvedAt` is nil (`IsOpen()`/`IsResolved()`), to avoid a second field
  that could drift out of sync.
- `incident.New(...)` opens an incident (`ResolvedAt` nil). `Resolve(t)` is
  the only way to close one; it errors on double-resolve
  (`ErrAlreadyResolved`) or `t` before `StartedAt`
  (`ErrResolvedBeforeStarted`). **Reopening is not supported** — a
  recurrence after resolution is a new `Incident`, by design.
- The failure-threshold/state-transition *engine* (deciding *when* N
  consecutive failures should open/resolve an incident) is explicitly
  deferred to M7. This type only owns the shape and the single
  open→resolved transition.
- `Duration(now time.Time) time.Duration` is derived, not stored.

## Important Decisions

- **IDs**: plain `string`, UUIDv4 generated via `internal/id` (stdlib
  `crypto/rand`, no `google/uuid` dependency). Domain objects generate
  their own ID at construction time, so a `Target`/`CheckResult`/
  `Incident` is always fully valid the instant it's built, without waiting
  on a database. Considered a distinct `TargetID` named type for
  compile-time safety on the `TargetID` fields in `checker`/`incident`,
  but deferred — it would force those packages to import `target` for no
  current benefit, since target/checker/incident have zero
  interdependencies as of M1.
- **No `internal/models` package**: domain types live with the package
  most responsible for producing/owning them (`Target`→`target`,
  `CheckResult`→`checker`, `Incident`→`incident`), matching
  `docs/architecture.md`'s component table. `internal/id` is the one
  shared package, but it's a leaf *utility* (no business meaning), not a
  models dumping ground.
- **`New...Params` structs instead of positional constructor args**: used
  in all three `New` functions specifically because adjacent
  same-typed parameters (`Interval`/`Timeout` both `time.Duration`;
  `TargetID`/`Reason` both `string`) are easy to swap by accident at a
  call site with positional args.
- **Time**: `time.Time` / `time.Duration` throughout, no custom wrapper.
  All timestamps normalized to UTC in constructors — this system is
  designed to eventually run checks from multiple regions, so avoiding
  timezone-dependent comparisons early matters.
- **Errors**: each package exports sentinel `Err...` values so callers/
  tests use `errors.Is` rather than string matching.
- No repository/persistence interfaces created yet (`TargetRepository`
  etc.) — no implementation or caller exists yet; those belong to M2/M6
  when there's an actual reason for them.

## Dependencies

None beyond the Go standard library. `go.mod` has no `require` block.

## Validation

Run from repo root:
```
gofmt -w .        # ok, only reformatted target_test.go alignment
go vet ./...      # ok
go build ./...    # ok
go test ./...     # ok — 27 subtests across internal/id, internal/target,
                  #      internal/checker, internal/incident, all passing
```
Binary manually verified to start (logs `infravex api-monitor starting
milestone=M1`) and shut down cleanly on SIGTERM.

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go
├── internal/
│   ├── target/       target.go, target_test.go, doc.go
│   ├── checker/      check_result.go, check_result_test.go, doc.go
│   ├── incident/     incident.go, incident_test.go, doc.go
│   ├── id/           id.go, id_test.go        (shared ID utility)
│   ├── scheduler/    doc.go only (not implemented)
│   ├── alert/        doc.go only (not implemented)
│   ├── storage/      doc.go only (not implemented)
│   └── api/          doc.go only (not implemented)
├── docs/architecture.md, development.md, roadmap.md
├── README.md, LICENSE, .gitignore, go.mod, CLAUDE.md
```

## Future Work

**M2 — Target Management** is next: create/read/update/delete for
`Target`, using the `target.New`/`target.Validate` already built in M1.
Expect an in-memory store first (no DB until M6) and the first real
question of where a repository-style interface should live, now that
there's an actual caller for one.
