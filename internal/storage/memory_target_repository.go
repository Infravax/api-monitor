package storage

import (
	"context"
	"sort"
	"sync"

	"github.com/InfraVex/api-monitor/internal/target"
)

// MemoryTargetRepository is an in-memory, concurrency-safe implementation
// of target.Repository. It exists to unblock the REST API before
// persistence (M6, PostgreSQL) arrives. Data does not survive a process
// restart.
type MemoryTargetRepository struct {
	mu      sync.RWMutex
	targets map[string]target.Target
}

// NewMemoryTargetRepository creates an empty MemoryTargetRepository.
func NewMemoryTargetRepository() *MemoryTargetRepository {
	return &MemoryTargetRepository{targets: make(map[string]target.Target)}
}

// Create stores a new target. It fails with target.ErrAlreadyExists if a
// target with the same ID is already stored (in practice this should not
// happen, since IDs are server-generated UUIDs, but Create should not
// silently overwrite an existing record if it ever did).
func (r *MemoryTargetRepository) Create(_ context.Context, t target.Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.targets[t.ID]; exists {
		return target.ErrAlreadyExists
	}
	r.targets[t.ID] = t
	return nil
}

// Get returns the target with the given ID, or target.ErrNotFound if none
// exists.
func (r *MemoryTargetRepository) Get(_ context.Context, id string) (target.Target, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.targets[id]
	if !ok {
		return target.Target{}, target.ErrNotFound
	}
	return t, nil
}

// List returns all stored targets, ordered by Name then ID for a
// deterministic result. The domain model has no creation timestamp (see
// docs/architecture.md), so name is the most meaningful stable sort key
// available.
func (r *MemoryTargetRepository) List(_ context.Context) ([]target.Target, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]target.Target, 0, len(r.targets))
	for _, t := range r.targets {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}

// Update replaces a stored target. It fails with target.ErrNotFound if no
// target with the given ID exists.
func (r *MemoryTargetRepository) Update(_ context.Context, t target.Target) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.targets[t.ID]; !ok {
		return target.ErrNotFound
	}
	r.targets[t.ID] = t
	return nil
}

// Delete removes a stored target. It fails with target.ErrNotFound if no
// target with the given ID exists.
func (r *MemoryTargetRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.targets[id]; !ok {
		return target.ErrNotFound
	}
	delete(r.targets, id)
	return nil
}
