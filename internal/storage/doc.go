// Package storage persists domain objects. Other packages depend on
// storage through interfaces they define themselves (not defined here), so
// the underlying persistence technology can change without forcing changes
// on its consumers.
//
// As of Milestone 2, it provides an in-memory implementation of
// target.Repository (MemoryTargetRepository). PostgreSQL is planned for
// M6, at which point a second implementation of the same interface will be
// added here.
package storage
