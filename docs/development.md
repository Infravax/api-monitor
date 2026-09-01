# Development Philosophy

These principles apply to every milestone of API Monitor, not just
Milestone 0. When in doubt, favor the option that keeps this list true.

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
problem the standard library doesn't (e.g., a Postgres driver in M6, since
`database/sql` needs one). Every dependency is a thing that can break,
need upgrading, or carry a vulnerability. Milestone 0 has zero non-stdlib
dependencies by design.

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
