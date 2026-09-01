package incident

import (
	"errors"
	"testing"
	"time"
)

func TestNew_ValidOpen(t *testing.T) {
	started := time.Now()

	inc, err := New(NewParams{TargetID: "t1", Reason: "3 consecutive failures", StartedAt: started})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if inc.ID == "" {
		t.Error("New() did not assign an ID")
	}
	if !inc.IsOpen() {
		t.Error("IsOpen() = false, want true for a freshly opened incident")
	}
	if inc.IsResolved() {
		t.Error("IsResolved() = true, want false for a freshly opened incident")
	}
	if inc.ResolvedAt != nil {
		t.Error("ResolvedAt should be nil for an open incident")
	}
}

func TestResolve_Valid(t *testing.T) {
	started := time.Now()
	inc, err := New(NewParams{TargetID: "t1", Reason: "3 consecutive failures", StartedAt: started})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	resolvedAt := started.Add(5 * time.Minute)
	if err := inc.Resolve(resolvedAt); err != nil {
		t.Fatalf("Resolve() unexpected error: %v", err)
	}
	if !inc.IsResolved() {
		t.Error("IsResolved() = false after Resolve()")
	}
	if inc.IsOpen() {
		t.Error("IsOpen() = true after Resolve()")
	}
	if inc.Duration(time.Now()) != 5*time.Minute {
		t.Errorf("Duration() = %v, want 5m", inc.Duration(time.Now()))
	}
}

func TestNew_Invalid(t *testing.T) {
	started := time.Now()

	tests := []struct {
		name    string
		params  NewParams
		wantErr error
	}{
		{
			name:    "empty target id",
			params:  NewParams{TargetID: "", Reason: "down", StartedAt: started},
			wantErr: ErrEmptyTargetID,
		},
		{
			name:    "empty reason",
			params:  NewParams{TargetID: "t1", Reason: "  ", StartedAt: started},
			wantErr: ErrEmptyReason,
		},
		{
			name:    "zero started at",
			params:  NewParams{TargetID: "t1", Reason: "down", StartedAt: time.Time{}},
			wantErr: ErrZeroStartedAt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.params)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestResolve_BeforeStarted(t *testing.T) {
	started := time.Now()
	inc, err := New(NewParams{TargetID: "t1", Reason: "down", StartedAt: started})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	if err := inc.Resolve(started.Add(-time.Minute)); !errors.Is(err, ErrResolvedBeforeStarted) {
		t.Fatalf("Resolve() error = %v, want %v", err, ErrResolvedBeforeStarted)
	}
}

func TestResolve_AlreadyResolved(t *testing.T) {
	started := time.Now()
	inc, err := New(NewParams{TargetID: "t1", Reason: "down", StartedAt: started})
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	if err := inc.Resolve(started.Add(time.Minute)); err != nil {
		t.Fatalf("first Resolve() unexpected error: %v", err)
	}
	if err := inc.Resolve(started.Add(2 * time.Minute)); !errors.Is(err, ErrAlreadyResolved) {
		t.Fatalf("second Resolve() error = %v, want %v", err, ErrAlreadyResolved)
	}
}

func TestValidate_InvalidStateCombinations(t *testing.T) {
	started := time.Now()
	beforeStart := started.Add(-time.Minute)

	tests := []struct {
		name     string
		incident Incident
		wantErr  error
	}{
		{
			name:     "empty id",
			incident: Incident{TargetID: "t1", Reason: "down", StartedAt: started},
			wantErr:  ErrEmptyID,
		},
		{
			name: "resolved before started",
			incident: Incident{
				ID: "i1", TargetID: "t1", Reason: "down",
				StartedAt: started, ResolvedAt: &beforeStart,
			},
			wantErr: ErrResolvedBeforeStarted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.incident.Validate(); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
