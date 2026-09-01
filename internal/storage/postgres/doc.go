// Package postgres is a PostgreSQL-backed implementation of
// target.Repository (internal/target), added in Milestone 6 as the second
// implementation of that interface — storage.MemoryTargetRepository (M2)
// is the first. target.Service cannot tell them apart; only main.go's
// wiring decides which one is used.
//
// It also owns connection pool setup (NewPool) and schema migrations
// (Migrate, via embedded SQL files in migrations/), which are concerns
// specific to this one backend rather than to storage in general — the
// reason this lives in its own subpackage instead of alongside
// MemoryTargetRepository in the parent storage package.
package postgres
