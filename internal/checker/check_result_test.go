package checker

import (
	"errors"
	"testing"
	"time"
)

func TestNew_ValidOutcomes(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		params      NewParams
		wantSuccess bool
	}{
		{
			name: "successful request",
			params: NewParams{
				TargetID: "t1", Timestamp: now, Outcome: OutcomeSuccess,
				StatusCode: 200, Latency: 84 * time.Millisecond,
			},
			wantSuccess: true,
		},
		{
			name: "http failure",
			params: NewParams{
				TargetID: "t1", Timestamp: now, Outcome: OutcomeUnexpectedStatus,
				StatusCode: 500, Latency: 120 * time.Millisecond,
			},
			wantSuccess: false,
		},
		{
			name: "timeout",
			params: NewParams{
				TargetID: "t1", Timestamp: now, Outcome: OutcomeTimeout,
				Latency: 5 * time.Second, ErrorMessage: "context deadline exceeded",
			},
			wantSuccess: false,
		},
		{
			name: "connection failure",
			params: NewParams{
				TargetID: "t1", Timestamp: now, Outcome: OutcomeConnectionError,
				Latency: 12 * time.Millisecond, ErrorMessage: "connection refused",
			},
			wantSuccess: false,
		},
		{
			name: "canceled",
			params: NewParams{
				TargetID: "t1", Timestamp: now, Outcome: OutcomeCanceled,
				Latency: 3 * time.Millisecond, ErrorMessage: "context canceled",
			},
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New(tt.params)
			if err != nil {
				t.Fatalf("New() unexpected error: %v", err)
			}
			if r.ID == "" {
				t.Error("New() did not assign an ID")
			}
			if got := r.Success(); got != tt.wantSuccess {
				t.Errorf("Success() = %v, want %v", got, tt.wantSuccess)
			}
		})
	}
}

func TestNew_Invalid(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		params  NewParams
		wantErr error
	}{
		{
			name:    "empty target id",
			params:  NewParams{TargetID: "", Timestamp: now, Outcome: OutcomeSuccess, StatusCode: 200},
			wantErr: ErrEmptyTargetID,
		},
		{
			name:    "zero timestamp",
			params:  NewParams{TargetID: "t1", Timestamp: time.Time{}, Outcome: OutcomeSuccess, StatusCode: 200},
			wantErr: ErrZeroTimestamp,
		},
		{
			name:    "invalid outcome",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: Outcome("bogus")},
			wantErr: ErrInvalidOutcome,
		},
		{
			name:    "negative latency",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeSuccess, StatusCode: 200, Latency: -time.Millisecond},
			wantErr: ErrNegativeLatency,
		},
		{
			name:    "success missing status code",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeSuccess, StatusCode: 0},
			wantErr: ErrStatusCodeRequired,
		},
		{
			name:    "success with out of range status code",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeSuccess, StatusCode: 999},
			wantErr: ErrInvalidStatusCode,
		},
		{
			name:    "timeout with status code",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeTimeout, StatusCode: 200, ErrorMessage: "timed out"},
			wantErr: ErrStatusCodeNotAllowed,
		},
		{
			name:    "timeout missing error message",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeTimeout, ErrorMessage: ""},
			wantErr: ErrErrorMessageRequired,
		},
		{
			name:    "success with error message",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeSuccess, StatusCode: 200, ErrorMessage: "should not be here"},
			wantErr: ErrErrorMessagePresent,
		},
		{
			name:    "canceled with status code",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeCanceled, StatusCode: 200, ErrorMessage: "context canceled"},
			wantErr: ErrStatusCodeNotAllowed,
		},
		{
			name:    "canceled missing error message",
			params:  NewParams{TargetID: "t1", Timestamp: now, Outcome: OutcomeCanceled, ErrorMessage: ""},
			wantErr: ErrErrorMessageRequired,
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

func TestValidate_EmptyID(t *testing.T) {
	r := CheckResult{
		TargetID: "t1", Timestamp: time.Now(), Outcome: OutcomeSuccess, StatusCode: 200,
	}
	if err := r.Validate(); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("Validate() error = %v, want %v", err, ErrEmptyID)
	}
}
