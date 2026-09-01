package checker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/InfraVex/api-monitor/internal/target"
)

// newTestTarget builds a valid target.Target through the normal domain
// constructor, so it carries whatever invariants target.New enforces.
func newTestTarget(t *testing.T, url, method string, timeout time.Duration, expectedStatusCode int) target.Target {
	t.Helper()
	tg, err := target.New(target.NewParams{
		Name:               "test target",
		URL:                url,
		Method:             method,
		Interval:           time.Minute,
		Timeout:            timeout,
		ExpectedStatusCode: expectedStatusCode,
	})
	if err != nil {
		t.Fatalf("target.New() unexpected error: %v", err)
	}
	return tg
}

func TestCheck_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", time.Second, http.StatusOK)
	c := NewChecker(nil)

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeSuccess)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.Latency < 0 {
		t.Errorf("Latency = %v, want non-negative", result.Latency)
	}
	if result.ErrorMessage != "" {
		t.Errorf("ErrorMessage = %q, want empty", result.ErrorMessage)
	}
	if !result.Success() {
		t.Error("Success() = false, want true")
	}
	if result.TargetID != tg.ID {
		t.Errorf("TargetID = %q, want %q", result.TargetID, tg.ID)
	}
}

func TestCheck_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", time.Second, http.StatusOK)
	c := NewChecker(nil)

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeUnexpectedStatus {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeUnexpectedStatus)
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
	if result.Success() {
		t.Error("Success() = true, want false")
	}
}

func TestCheck_DifferentExpectedStatus(t *testing.T) {
	// Proves the checker compares against the target's configured
	// expectation rather than hard-coding 200.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", time.Second, http.StatusCreated)
	c := NewChecker(nil)

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeSuccess)
	}
	if result.StatusCode != http.StatusCreated {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusCreated)
	}
}

func TestCheck_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", 50*time.Millisecond, http.StatusOK)
	c := NewChecker(nil)

	start := time.Now()
	result := c.Check(context.Background(), tg)
	elapsed := time.Since(start)

	if result.Outcome != OutcomeTimeout {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeTimeout)
	}
	if result.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", result.StatusCode)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage is empty, want a description of the timeout")
	}
	// Proves Check does not hang indefinitely: it should return close to
	// the target's own 50ms timeout, nowhere near the server's 300ms sleep.
	if elapsed > 2*time.Second {
		t.Errorf("Check took %v, want well under the server's 300ms sleep + margin", elapsed)
	}
}

func TestCheck_ConnectionRefused(t *testing.T) {
	// A server that is started and then immediately closed leaves nothing
	// listening on that address, giving a deterministic, local-only
	// "connection refused" without depending on external network access.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadAddr := server.URL
	server.Close()

	tg := newTestTarget(t, deadAddr, "GET", 2*time.Second, http.StatusOK)
	c := NewChecker(nil)

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeConnectionError {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeConnectionError)
	}
	if result.StatusCode != 0 {
		t.Errorf("StatusCode = %d, want 0", result.StatusCode)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage is empty, want a description of the connection failure")
	}
}

func TestCheck_ContextCanceledBeforeStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", 5*time.Second, http.StatusOK)
	c := NewChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before Check is ever called

	start := time.Now()
	result := c.Check(ctx, tg)
	elapsed := time.Since(start)

	if result.Outcome != OutcomeCanceled {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeCanceled)
	}
	if elapsed > time.Second {
		t.Errorf("Check took %v, want near-instant given an already-canceled context", elapsed)
	}
}

func TestCheck_ContextCanceledDuringRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // blocks until the client cancels
	}))
	defer server.Close()

	// A generous target timeout, so if OutcomeCanceled fires it's clearly
	// because of the explicit cancel below, not a race with t.Timeout.
	tg := newTestTarget(t, server.URL, "GET", 5*time.Second, http.StatusOK)
	c := NewChecker(nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	result := c.Check(ctx, tg)
	elapsed := time.Since(start)

	if result.Outcome != OutcomeCanceled {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeCanceled)
	}
	if elapsed > time.Second {
		t.Errorf("Check took %v, want prompt termination near the 30ms cancel, not the 5s target timeout", elapsed)
	}
}

func TestCheck_InvalidURL(t *testing.T) {
	// target.Validate() would normally reject a URL like this, so this
	// simulates a Target reaching the checker without having gone through
	// validation (e.g. a future bug elsewhere), constructed directly
	// rather than via target.New. Check must not panic.
	tg := target.Target{
		ID:                 "t1",
		Name:               "bad",
		URL:                "://bad-url",
		Method:             "GET",
		Interval:           time.Minute,
		Timeout:            time.Second,
		ExpectedStatusCode: 200,
	}
	c := NewChecker(nil)

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeConnectionError {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeConnectionError)
	}
	if result.ErrorMessage == "" {
		t.Error("ErrorMessage is empty, want a description of the failure")
	}
}

func TestCheck_Methods(t *testing.T) {
	methods := []string{"GET", "POST"}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			var gotMethod string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			tg := newTestTarget(t, server.URL, method, time.Second, http.StatusOK)
			c := NewChecker(nil)

			result := c.Check(context.Background(), tg)

			if gotMethod != method {
				t.Errorf("server observed method %q, want %q", gotMethod, method)
			}
			if result.Outcome != OutcomeSuccess {
				t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeSuccess)
			}
		})
	}
}

func TestCheck_HTTPS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", time.Second, http.StatusOK)
	// server.Client() trusts this test server's specific certificate
	// (added to its own RootCAs), not certificates in general — this
	// verifies real TLS validation, unlike InsecureSkipVerify.
	c := NewChecker(server.Client())

	result := c.Check(context.Background(), tg)

	if result.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeSuccess)
	}
}

func TestCheck_Concurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	tg := newTestTarget(t, server.URL, "GET", time.Second, http.StatusOK)
	c := NewChecker(nil)

	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := c.Check(context.Background(), tg)
			if result.Outcome != OutcomeSuccess {
				t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeSuccess)
			}
		}()
	}
	wg.Wait()
}
