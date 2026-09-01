package target

import (
	"context"
	"errors"
)

// Sentinel errors for Repository outcomes.
var (
	ErrNotFound      = errors.New("target: not found")
	ErrAlreadyExists = errors.New("target: already exists")
)

// Repository persists Targets. It is defined here, in the package that
// owns the Target type, and implemented elsewhere — an in-memory
// implementation lives in internal/storage for now, with PostgreSQL
// planned for M6 — so this package and its callers never depend on a
// specific storage technology.
//
// This interface exists because Service (below) needs it right now; it is
// not speculative.
type Repository interface {
	Create(ctx context.Context, t Target) error
	Get(ctx context.Context, id string) (Target, error)
	List(ctx context.Context) ([]Target, error)
	Update(ctx context.Context, t Target) error
	Delete(ctx context.Context, id string) error
}
