package api

import (
	"fmt"
	"time"

	"github.com/InfraVex/api-monitor/internal/target"
)

// targetRequest is the wire representation of a target in create/update
// requests. It exists separately from target.Target for two concrete
// reasons, not speculative layering:
//
//  1. Interval/Timeout are human-friendly duration strings ("30s") on the
//     wire, but time.Duration internally — encoding/json has no built-in
//     support for that conversion, so something has to do it explicitly.
//  2. It decouples the external JSON contract from the domain type's Go
//     field names/shape, so the wire format can evolve independently of
//     the domain model.
//
// The same struct is used for both create and update: both accept a full
// target representation (PUT is a full replace, and create has no partial
// fields either).
type targetRequest struct {
	Name               string `json:"name"`
	URL                string `json:"url"`
	Method             string `json:"method"`
	Interval           string `json:"interval"`
	Timeout            string `json:"timeout"`
	ExpectedStatusCode int    `json:"expected_status_code"`
	Enabled            *bool  `json:"enabled"`
}

// enabled returns the requested Enabled value, defaulting to true when the
// field is omitted from the request body.
func (r targetRequest) enabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// durations parses Interval/Timeout. An empty string parses to 0, which
// target.Validate will reject with a domain-specific error — duration
// presence/positivity is intentionally checked in one place (the domain
// layer), not duplicated here.
func (r targetRequest) durations() (interval, timeout time.Duration, err error) {
	if r.Interval != "" {
		interval, err = time.ParseDuration(r.Interval)
		if err != nil {
			return 0, 0, fmt.Errorf("interval: %w", err)
		}
	}
	if r.Timeout != "" {
		timeout, err = time.ParseDuration(r.Timeout)
		if err != nil {
			return 0, 0, fmt.Errorf("timeout: %w", err)
		}
	}
	return interval, timeout, nil
}

// toNewParams converts the request into target.NewParams for creation.
func (r targetRequest) toNewParams() (target.NewParams, error) {
	interval, timeout, err := r.durations()
	if err != nil {
		return target.NewParams{}, err
	}
	return target.NewParams{
		Name:               r.Name,
		URL:                r.URL,
		Method:             r.Method,
		Interval:           interval,
		Timeout:            timeout,
		ExpectedStatusCode: r.ExpectedStatusCode,
	}, nil
}

// toUpdateParams converts the request into target.UpdateParams.
func (r targetRequest) toUpdateParams() (target.UpdateParams, error) {
	interval, timeout, err := r.durations()
	if err != nil {
		return target.UpdateParams{}, err
	}
	return target.UpdateParams{
		Name:               r.Name,
		URL:                r.URL,
		Method:             r.Method,
		Interval:           interval,
		Timeout:            timeout,
		ExpectedStatusCode: r.ExpectedStatusCode,
		Enabled:            r.enabled(),
	}, nil
}

// targetResponse is the wire representation of a target returned to
// clients.
type targetResponse struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	Method             string `json:"method"`
	Interval           string `json:"interval"`
	Timeout            string `json:"timeout"`
	ExpectedStatusCode int    `json:"expected_status_code"`
	Enabled            bool   `json:"enabled"`
}

func toTargetResponse(t target.Target) targetResponse {
	return targetResponse{
		ID:                 t.ID,
		Name:               t.Name,
		URL:                t.URL,
		Method:             t.Method,
		Interval:           t.Interval.String(),
		Timeout:            t.Timeout.String(),
		ExpectedStatusCode: t.ExpectedStatusCode,
		Enabled:            t.Enabled,
	}
}

type targetListResponse struct {
	Targets []targetResponse `json:"targets"`
}
