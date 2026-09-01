// Package storage will persist targets, check results, and incidents, and
// provide the read paths needed for history and reporting.
//
// Other packages depend on storage through interfaces they define
// themselves (not defined here), so the underlying persistence technology
// (starting with PostgreSQL, per the roadmap) can change without forcing
// changes on its consumers.
//
// Not yet implemented (Milestone 0 is documentation-only for this package).
package storage
