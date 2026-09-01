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
Current Milestone: M3 (complete)
```

## Completed Milestones

```
M0 — Foundation & Architecture
M1 — Domain Model
M2 — Target Management + REST Backend + Docker Foundation
M3 — HTTP Checker
```

## Current Architecture

Modular monolith. Full design in `docs/architecture.md` — see its "The
HTTP Checker (Milestone 3)" section for what M3 actually added, and
"Request architecture (Milestone 2)" for the REST API path.

**What's actually wired into the running application** (`cmd/api-monitor`):
```
net/http server → RequestID/Logging/Recovery middleware → TargetHandler
       → target.Service → Target.Validate → target.Repository
       → storage.MemoryTargetRepository
```

**What exists as a tested, callable component but is NOT wired into the
running application yet:**
```
target.Target → checker.Checker.Check(ctx, target) → checker.CheckResult
```
`checker.Checker` is fully implemented and tested (see M3 Implementation
below), but nothing in `main.go` constructs one or calls `Check`. There is
no scheduler (M4) to invoke it periodically, no worker pool (M5) to run it
at scale, nothing persists its output (M6), and nothing reacts to it
(M7/M8). It is reachable today only from its own package tests.

`scheduler`, `incident`, `alert` remain doc-only. `incident.Incident` (M1)
is implemented but still has no consumer.

## Domain Model

- **`Target`** (`internal/target`) — unchanged from M1/M2.
- **`CheckResult`** (`internal/checker`) — **changed in M3**: added one new
  `Outcome` value, `OutcomeCanceled` (`"canceled"`), alongside the
  existing `success`/`unexpected_status`/`timeout`/`connection_error`.
  Justification: the M1 enum couldn't distinguish "the target's own
  configured `Timeout` elapsed" from "the caller aborted the check for
  operational reasons" (e.g. process shutdown) — conflating them would
  misreport an operational abort as target slowness in future
  incident-detection data. `OutcomeCanceled` follows the same validation
  rules as `timeout`/`connection_error` (no `StatusCode`, non-empty
  `ErrorMessage`). This was a deliberately minimal, justified addition —
  not a redesign; `Validate()`'s structure and `Success()` derivation are
  unchanged.
- **`Incident`** (`internal/incident`) — unchanged from M1, still unused
  by anything.

## M3 Implementation

- **`checker.Checker`** (`internal/checker/check.go`) — a concrete
  struct wrapping `*http.Client`, not an interface: exactly one
  implementation exists and no current caller needs to substitute
  another, so an interface would be unjustified abstraction.
  `checker.NewChecker(client *http.Client) *Checker` — named `NewChecker`,
  not `New`, because `New` already exists in this package as
  `CheckResult`'s M1 constructor. Passing `nil` yields a plain
  `&http.Client{}`.
- **`Check(ctx context.Context, t target.Target) CheckResult`** — the
  checker's only method. Returns a `CheckResult` only, never a Go
  `error`: any failure to reach `t` or get the expected response *is* the
  observation being reported, and lives in the returned value's
  `Outcome`/`ErrorMessage`, not a separate error channel (this mirrors
  `CheckResult`'s M1 no-contradictory-state design).
- **Timeout strategy**: `context.WithTimeout(ctx, t.Timeout)` per call,
  layered on top of whatever `ctx` already carries — **not**
  `http.Client.Timeout`. Reason: one shared `*http.Client` is reused
  across targets with different configured `Timeout` values; a single
  fixed `Client.Timeout` field can't represent that. `checkCtx.Err()` /
  the error from `client.Do` is classified via `errors.Is`:
  `context.Canceled` → `OutcomeCanceled`; `context.DeadlineExceeded` →
  `OutcomeTimeout`; anything else → `OutcomeConnectionError`. This
  correctly distinguishes an externally-canceled `ctx` from `t.Timeout`
  itself elapsing (see Domain Model above).
- **Latency measurement**: `time.Since(start)` computed immediately after
  `client.Do` returns (i.e. once response headers arrive), **before**
  draining the body. Full body-transfer time is deliberately excluded —
  M3 doesn't validate bodies, so counting drain time would make latency
  numbers misleading for large-but-healthy responses. `start` is recorded
  at the very top of `Check`, so even a request-construction failure gets
  a coherent (near-zero, non-negative) latency.
- **Status-code handling**: compares `resp.StatusCode` against
  `t.ExpectedStatusCode` directly — no hardcoded `200`/2xx assumption
  (`TestCheck_DifferentExpectedStatus` proves this with `201`). A
  defensive guard rejects `resp.StatusCode` outside 100–599 (Go's HTTP
  parser only guarantees a 3-digit code, not that range) as
  `OutcomeConnectionError`, so a malformed/unusual server response can
  never violate `CheckResult.Validate()`'s own invariant.
- **Redirects**: standard `net/http` `Client` default policy (follow, cap
  10). No custom `CheckRedirect` — most real APIs redirect legitimately
  (e.g. http→https), and not following would misreport a healthy
  redirect-based setup as a failure.
- **TLS**: default system trust store, no `InsecureSkipVerify`. A cert
  failure surfaces as a wrapped error, falling into the default
  `OutcomeConnectionError` classification branch — no special-casing
  needed, matches that outcome's existing M1 doc comment
  ("...TLS error, etc.").
- **Response body**: read via `io.Copy(io.Discard, io.LimitReader(body,
  64*1024))` then closed, always via `defer`. Reason: draining (not just
  closing) lets `net/http`'s transport return the connection to its
  keep-alive pool for reuse on a future check against the same target
  (relevant once M4 schedules repeated checks); the 64 KiB cap bounds
  time/memory spent on unexpectedly large or slow bodies. `io.Discard`
  never buffers the content regardless of cap size. A drain-read error is
  deliberately ignored — headers/status were already observed by that
  point, which is what M3 reports on.
- **`finish` helper panics on `New`/`Validate` failure**: this is reached
  only if `Check`'s own classification logic produced an internally
  inconsistent combination of fields — a bug in `check.go`, not a runtime
  condition external callers should have to handle (same reasoning as
  `internal/id`'s panic-on-`rand.Read`-failure from M1). The 100–599
  status guard above exists specifically so this panic path can never be
  reached by adversarial/unusual *external* server input — only by an
  actual bug in this file.

## Dependencies

None beyond the Go standard library. `go.mod` has no `require` block, no
`go.sum` exists. (Unchanged since M0.)

## Verification

Run from repo root — all actually executed for M3:
```
gofmt -w .            # ok, only reformatted whitespace/alignment
go vet ./...           # ok
go build ./...          # ok
go test ./...            # ok — all packages pass, including 12 new
                          #      checker tests (success, unexpected status,
                          #      different expected status, timeout,
                          #      connection refused, cancel-before-start,
                          #      cancel-during-request, invalid URL,
                          #      GET/POST methods, HTTPS, concurrency)
go test -race ./...       # ok — no data races
```
All checker tests use `httptest.NewServer`/`NewTLSServer` — no external
network dependency, fully deterministic. The HTTPS test uses
`server.Client()` (which trusts only that test server's own certificate)
rather than `InsecureSkipVerify`, so it exercises real TLS verification.

Docker was not touched this milestone (no changes were needed) and
remains unverified in this environment (no Docker daemon available — see
M2 notes).

## Current Repository Structure

```
api-monitor/
├── cmd/api-monitor/main.go              (wiring only — does not call checker yet)
├── internal/
│   ├── target/    target.go, repository.go, service.go, doc.go + tests
│   ├── checker/   check_result.go, check.go (NEW M3), doc.go + tests
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

**M4 — Scheduler** is next: periodically trigger `checker.Checker.Check`
for each enabled `Target`, based on its `Interval`. This is the milestone
that actually wires the Checker into the running application for the
first time. Expect the first real question of how scheduled work should
be represented (timers vs. a single loop with a priority queue) and
whether the scheduler calls `Checker` directly or through some queuing
mechanism — the roadmap defers worker pools to M5 and any message broker
to M9, so M4 itself should stay a single-process, in-memory scheduling
loop.
