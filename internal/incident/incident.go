package incident

import (
	"errors"
	"strings"
	"time"

	"github.com/InfraVex/api-monitor/internal/id"
)

// Incident represents a period during which a target is considered
// unhealthy: from the moment it is opened until it is resolved, if ever.
//
// There is no separate Status field: ResolvedAt being nil or non-nil is the
// single source of truth for open vs. resolved (see IsOpen/IsResolved), so
// the two can never disagree with each other.
type Incident struct {
	ID         string
	TargetID   string
	Reason     string
	StartedAt  time.Time
	ResolvedAt *time.Time
}

// Sentinel errors for Incident validation and transition failures.
var (
	ErrEmptyID               = errors.New("incident: id is required")
	ErrEmptyTargetID         = errors.New("incident: target id is required")
	ErrEmptyReason           = errors.New("incident: reason is required")
	ErrZeroStartedAt         = errors.New("incident: started at is required")
	ErrResolvedBeforeStarted = errors.New("incident: resolved at cannot be before started at")
	ErrAlreadyResolved       = errors.New("incident: incident is already resolved")
)

// NewParams holds the inputs for New.
type NewParams struct {
	TargetID  string
	Reason    string
	StartedAt time.Time
}

// New opens a new incident for a target at StartedAt, explaining why it was
// opened. The returned incident is unresolved (ResolvedAt is nil).
func New(p NewParams) (Incident, error) {
	i := Incident{
		ID:        id.New(),
		TargetID:  p.TargetID,
		Reason:    strings.TrimSpace(p.Reason),
		StartedAt: p.StartedAt.UTC(),
	}
	if err := i.Validate(); err != nil {
		return Incident{}, err
	}
	return i, nil
}

// IsOpen reports whether the incident has not yet been resolved.
func (i Incident) IsOpen() bool {
	return i.ResolvedAt == nil
}

// IsResolved reports whether the incident has been resolved.
func (i Incident) IsResolved() bool {
	return i.ResolvedAt != nil
}

// Duration returns how long the incident has been open. For a resolved
// incident this is fixed; for an open incident it is measured against now,
// which the caller supplies so Duration stays a pure function of its
// inputs.
func (i Incident) Duration(now time.Time) time.Duration {
	if i.ResolvedAt != nil {
		return i.ResolvedAt.Sub(i.StartedAt)
	}
	return now.Sub(i.StartedAt)
}

// Resolve marks an open incident as resolved at resolvedAt. It fails if the
// incident is already resolved or if resolvedAt precedes StartedAt.
//
// Resolving twice, or "reopening" a resolved incident, is intentionally not
// supported: a recurrence after resolution is modeled as a new incident.
// The threshold/flapping logic that decides *when* to open or resolve an
// incident belongs to the incident engine (M7), not this type.
func (i *Incident) Resolve(resolvedAt time.Time) error {
	if i.IsResolved() {
		return ErrAlreadyResolved
	}
	resolvedAt = resolvedAt.UTC()
	if resolvedAt.Before(i.StartedAt) {
		return ErrResolvedBeforeStarted
	}
	i.ResolvedAt = &resolvedAt
	return nil
}

// Validate checks that the Incident satisfies all domain invariants.
func (i Incident) Validate() error {
	if i.ID == "" {
		return ErrEmptyID
	}
	if i.TargetID == "" {
		return ErrEmptyTargetID
	}
	if i.Reason == "" {
		return ErrEmptyReason
	}
	if i.StartedAt.IsZero() {
		return ErrZeroStartedAt
	}
	if i.ResolvedAt != nil && i.ResolvedAt.Before(i.StartedAt) {
		return ErrResolvedBeforeStarted
	}
	return nil
}
