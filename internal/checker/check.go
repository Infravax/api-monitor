package checker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/InfraVex/api-monitor/internal/target"
)

// maxDrainBytes bounds how much of a response body Check reads before
// discarding it. M3 does not validate response content — the body is
// drained only so the underlying TCP/TLS connection can be returned to the
// client's keep-alive pool instead of being torn down, which will matter
// once checks against the same target repeat on a schedule (M4). The cap
// bounds time/attacker-controlled input spent on unexpectedly large or
// slow bodies; io.Discard itself does not buffer the content regardless of
// how much is copied through it.
const maxDrainBytes = 64 * 1024

// Checker performs HTTP/HTTPS checks against targets.
//
// It is a concrete type, not an interface: there is exactly one
// implementation of "perform an HTTP check" in this codebase and no
// current caller needs to swap it out, so an interface would be
// abstraction without a reason (see docs/development.md).
//
// Checker holds no mutable state beyond its *http.Client, which is itself
// documented as safe for concurrent use by multiple goroutines, so Check
// is safe to call concurrently.
type Checker struct {
	client *http.Client
}

// NewChecker creates a Checker. client is accepted as a dependency, rather
// than constructed internally, for two reasons: production callers may
// want to tune transport-level behavior (connection pool limits, proxies),
// and tests need to inject an httptest server's own client (e.g.
// server.Client() for TLS tests) rather than relying on a global override
// like InsecureSkipVerify. Pass nil to get a plain *http.Client{}.
//
// It is named NewChecker, not New, because New already exists in this
// package as CheckResult's constructor (see check_result.go).
//
// The returned Checker's client intentionally has no client-wide Timeout:
// each Check call supplies its own per-request deadline derived from the
// target being checked (see Check), since a single shared client is reused
// across targets with different configured timeouts — a single fixed
// http.Client.Timeout could not represent that.
func NewChecker(client *http.Client) *Checker {
	if client == nil {
		client = &http.Client{}
	}
	return &Checker{client: client}
}

// Check performs a single HTTP/HTTPS check against t and returns the
// result.
//
// Check never returns a Go error: any failure to reach t or to receive the
// expected response from it is itself the observation being reported, and
// is represented in the returned CheckResult's Outcome/ErrorMessage rather
// than a separate error the caller must branch on (see CheckResult's
// no-contradictory-state design in check_result.go). Check answers "what
// happened," not "is the target healthy" — that judgment belongs to the
// future incident engine (M7), not here.
//
// t.Timeout bounds this specific check, layered on top of whatever ctx
// already carries: context.WithTimeout(ctx, t.Timeout). This is
// deliberately per-call context, not http.Client.Timeout, because the
// client is shared across targets with different Timeout values. If ctx is
// canceled externally before t.Timeout elapses — e.g. the process is
// shutting down — that is reported as OutcomeCanceled, distinct from
// OutcomeTimeout (t's own configured budget running out): conflating the
// two would misreport an operational abort as the target itself being
// slow.
func (c *Checker) Check(ctx context.Context, t target.Target) CheckResult {
	start := time.Now()

	checkCtx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, t.Method, t.URL, nil)
	if err != nil {
		return c.finish(t.ID, start, time.Since(start), OutcomeConnectionError, 0,
			fmt.Sprintf("request could not be constructed: %v", err))
	}

	resp, err := c.client.Do(req)
	latency := time.Since(start)
	if err != nil {
		outcome := classify(err)
		return c.finish(t.ID, start, latency, outcome, 0, err.Error())
	}
	defer drainAndClose(resp.Body)

	// Go's HTTP client only guarantees a 3-digit status code, not that it
	// falls in the conventional 100-599 range CheckResult requires (see
	// its Validate). A server sending something outside that range is
	// treated the same as any other response we can't trust, rather than
	// passed through and violating CheckResult's own invariant.
	if resp.StatusCode < 100 || resp.StatusCode > 599 {
		return c.finish(t.ID, start, latency, OutcomeConnectionError, 0,
			fmt.Sprintf("received non-standard status code %d", resp.StatusCode))
	}

	outcome := OutcomeUnexpectedStatus
	if resp.StatusCode == t.ExpectedStatusCode {
		outcome = OutcomeSuccess
	}
	return c.finish(t.ID, start, latency, outcome, resp.StatusCode, "")
}

// classify maps an error returned by http.Client.Do to an Outcome, using
// errors.Is against the context package's sentinels rather than string
// matching — net/http wraps context errors in a way that satisfies
// errors.Is through *url.Error's Unwrap.
func classify(err error) Outcome {
	switch {
	case errors.Is(err, context.Canceled):
		return OutcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return OutcomeTimeout
	default:
		return OutcomeConnectionError
	}
}

// drainAndClose reads and discards up to maxDrainBytes of body so the
// connection can be reused, then closes it. Any read error is deliberately
// ignored: by this point a response with headers/status was already
// observed, which is what this milestone reports on; a body-read failure
// afterward doesn't change that observation (see docs/architecture.md).
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
	_ = body.Close()
}

// finish builds the CheckResult via New, so ID generation and timestamp
// normalization stay in one place. A New error here would mean Check
// itself constructed an inconsistent combination of fields — a bug in this
// file, not a runtime condition — so it panics rather than forcing every
// caller of Check to handle an "impossible" error return.
func (c *Checker) finish(targetID string, timestamp time.Time, latency time.Duration, outcome Outcome, statusCode int, errMsg string) CheckResult {
	r, err := New(NewParams{
		TargetID:     targetID,
		Timestamp:    timestamp,
		Outcome:      outcome,
		StatusCode:   statusCode,
		Latency:      latency,
		ErrorMessage: errMsg,
	})
	if err != nil {
		panic(fmt.Sprintf("checker: internal invariant violation building CheckResult: %v", err))
	}
	return r
}
