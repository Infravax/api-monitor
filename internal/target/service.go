package target

import (
	"context"
	"strings"
	"time"
)

// Service implements the target management use cases (create, read,
// update, delete) on top of a Repository. It knows nothing about HTTP:
// no status codes, no JSON, no request/response types. That belongs to
// the transport layer (internal/api) that calls it.
type Service struct {
	repo Repository
}

// NewService creates a Service backed by repo.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create validates and stores a new Target, applying the same defaults as
// New. The created target is always enabled, matching New's behavior.
func (s *Service) Create(ctx context.Context, p NewParams) (Target, error) {
	t, err := New(p)
	if err != nil {
		return Target{}, err
	}
	if err := s.repo.Create(ctx, t); err != nil {
		return Target{}, err
	}
	return t, nil
}

// Get retrieves a Target by ID. It returns ErrNotFound if no such target
// exists.
func (s *Service) Get(ctx context.Context, id string) (Target, error) {
	return s.repo.Get(ctx, id)
}

// List returns all known targets.
func (s *Service) List(ctx context.Context) ([]Target, error) {
	return s.repo.List(ctx)
}

// UpdateParams holds the inputs for Update. Unlike NewParams, it includes
// Enabled explicitly: a client must be able to pause or resume an existing
// target, whereas a newly created target always starts enabled (see New).
type UpdateParams struct {
	Name               string
	URL                string
	Method             string
	Interval           time.Duration
	Timeout            time.Duration
	ExpectedStatusCode int
	Enabled            bool
}

// Update replaces an existing target's configuration. It fails with
// ErrNotFound if the target does not exist, or with the relevant domain
// validation error if the new configuration is invalid. The target's ID is
// preserved; Validate (not New) is used so no new ID is generated.
func (s *Service) Update(ctx context.Context, id string, p UpdateParams) (Target, error) {
	existing, err := s.repo.Get(ctx, id)
	if err != nil {
		return Target{}, err
	}

	updated := Target{
		ID:                 existing.ID,
		Name:               strings.TrimSpace(p.Name),
		URL:                strings.TrimSpace(p.URL),
		Method:             normalizeMethod(p.Method),
		Interval:           p.Interval,
		Timeout:            p.Timeout,
		ExpectedStatusCode: normalizeExpectedStatusCode(p.ExpectedStatusCode),
		Enabled:            p.Enabled,
	}

	if err := updated.Validate(); err != nil {
		return Target{}, err
	}
	if err := s.repo.Update(ctx, updated); err != nil {
		return Target{}, err
	}
	return updated, nil
}

// Delete removes a target. It returns ErrNotFound if no such target
// exists.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.repo.Delete(ctx, id)
}
