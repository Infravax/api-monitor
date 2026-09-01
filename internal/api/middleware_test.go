package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequestID_SetsHeaderAndContext(t *testing.T) {
	var seenInContext string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenInContext = requestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	RequestID(inner).ServeHTTP(rr, req)

	headerID := rr.Header().Get("X-Request-ID")
	if headerID == "" {
		t.Fatal("X-Request-ID header not set")
	}
	if seenInContext != headerID {
		t.Errorf("request ID in context = %q, want %q (matching header)", seenInContext, headerID)
	}
}

func TestRecovery_RecoversFromPanic(t *testing.T) {
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	// Should not panic the test process.
	Recovery(discardLogger())(panicking).ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}

	var body errorEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error.Code != codeInternalError {
		t.Errorf("error code = %q, want %q", body.Error.Code, codeInternalError)
	}
}

func TestLogging_ReportsRecoveredStatus(t *testing.T) {
	// Logging must wrap Recovery so a recovered panic still yields the
	// correct (500) status in the log line, not the http.StatusOK default.
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := chain(panicking, Logging(discardLogger()), Recovery(discardLogger()))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
