package checker

import (
	"errors"
	"time"

	"github.com/InfraVex/api-monitor/internal/id"
)

// Outcome classifies the result of a single check attempt.
type Outcome string

const (
	// OutcomeSuccess means a response was received and its status code
	// matched the target's expected status code.
	OutcomeSuccess Outcome = "success"
	// OutcomeUnexpectedStatus means a response was received but its
	// status code did not match the target's expected status code.
	OutcomeUnexpectedStatus Outcome = "unexpected_status"
	// OutcomeTimeout means no response was received before the target's
	// configured timeout elapsed.
	OutcomeTimeout Outcome = "timeout"
	// OutcomeConnectionError means the request could not be completed at
	// all (DNS failure, connection refused, TLS error, etc.), as opposed
	// to a response that simply never arrived in time.
	OutcomeConnectionError Outcome = "connection_error"
	// OutcomeCanceled means the check was aborted by the caller (e.g.
	// process shutdown) before it could complete, as distinct from the
	// target's own configured timeout elapsing. Conflating the two would
	// misreport an operational abort as evidence the target is slow.
	OutcomeCanceled Outcome = "canceled"
)

func (o Outcome) valid() bool {
	switch o {
	case OutcomeSuccess, OutcomeUnexpectedStatus, OutcomeTimeout, OutcomeConnectionError, OutcomeCanceled:
		return true
	default:
		return false
	}
}

// CheckResult is the outcome of one monitoring attempt against a target.
//
// Success is intentionally not a stored field: it is derived from Outcome
// (see Success) so a CheckResult can never represent a contradictory state,
// such as being "successful" while also carrying an error message.
type CheckResult struct {
	ID           string
	TargetID     string
	Timestamp    time.Time
	Outcome      Outcome
	StatusCode   int
	Latency      time.Duration
	ErrorMessage string
}

// Sentinel errors for CheckResult validation failures.
var (
	ErrEmptyID              = errors.New("checker: id is required")
	ErrEmptyTargetID        = errors.New("checker: target id is required")
	ErrZeroTimestamp        = errors.New("checker: timestamp is required")
	ErrInvalidOutcome       = errors.New("checker: outcome is invalid")
	ErrNegativeLatency      = errors.New("checker: latency cannot be negative")
	ErrStatusCodeRequired   = errors.New("checker: status code is required when outcome is success or unexpected_status")
	ErrStatusCodeNotAllowed = errors.New("checker: status code must be absent when outcome is timeout, connection_error, or canceled")
	ErrInvalidStatusCode    = errors.New("checker: status code must be between 100 and 599")
	ErrErrorMessageRequired = errors.New("checker: error message is required when outcome is timeout, connection_error, or canceled")
	ErrErrorMessagePresent  = errors.New("checker: error message must be empty when outcome is success")
)

// NewParams holds the inputs for New.
type NewParams struct {
	TargetID     string
	Timestamp    time.Time
	Outcome      Outcome
	StatusCode   int
	Latency      time.Duration
	ErrorMessage string
}

// New creates a CheckResult and validates it before returning it.
func New(p NewParams) (CheckResult, error) {
	r := CheckResult{
		ID:           id.New(),
		TargetID:     p.TargetID,
		Timestamp:    p.Timestamp.UTC(),
		Outcome:      p.Outcome,
		StatusCode:   p.StatusCode,
		Latency:      p.Latency,
		ErrorMessage: p.ErrorMessage,
	}
	if err := r.Validate(); err != nil {
		return CheckResult{}, err
	}
	return r, nil
}

// Success reports whether the check is considered successful.
func (r CheckResult) Success() bool {
	return r.Outcome == OutcomeSuccess
}

// Validate checks that the CheckResult satisfies all domain invariants.
func (r CheckResult) Validate() error {
	if r.ID == "" {
		return ErrEmptyID
	}
	if r.TargetID == "" {
		return ErrEmptyTargetID
	}
	if r.Timestamp.IsZero() {
		return ErrZeroTimestamp
	}
	if !r.Outcome.valid() {
		return ErrInvalidOutcome
	}
	if r.Latency < 0 {
		return ErrNegativeLatency
	}

	switch r.Outcome {
	case OutcomeSuccess, OutcomeUnexpectedStatus:
		if r.StatusCode == 0 {
			return ErrStatusCodeRequired
		}
		if r.StatusCode < 100 || r.StatusCode > 599 {
			return ErrInvalidStatusCode
		}
	case OutcomeTimeout, OutcomeConnectionError, OutcomeCanceled:
		if r.StatusCode != 0 {
			return ErrStatusCodeNotAllowed
		}
		if r.ErrorMessage == "" {
			return ErrErrorMessageRequired
		}
	}

	if r.Outcome == OutcomeSuccess && r.ErrorMessage != "" {
		return ErrErrorMessagePresent
	}

	return nil
}
