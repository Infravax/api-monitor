// Package storage persists domain objects. Other packages depend on
// storage through interfaces they define themselves (not defined here), so
// the underlying persistence technology can change without forcing changes
// on its consumers.
//
// It provides MemoryTargetRepository, an in-memory implementation of
// target.Repository (M2).
//
// The PostgreSQL implementation of the same interface, added in M6, lives
// in the postgres subpackage (internal/storage/postgres), not here —
// unlike MemoryTargetRepository, it needs its own connection pool setup
// and embedded SQL migrations, concerns specific to that one backend
// rather than to "storage" in general. Both implementations satisfy
// target.Repository identically, so target.Service cannot tell them apart.
package storage
