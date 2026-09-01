package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/InfraVex/api-monitor/internal/storage"
	"github.com/InfraVex/api-monitor/internal/target"
)

// newTestRouter wires a fresh in-memory repository and service per test, so
// tests don't share state, and returns the full router (including
// middleware) so path parameters like {id} are populated the same way they
// would be in production.
func newTestRouter() http.Handler {
	repo := storage.NewMemoryTargetRepository()
	svc := target.NewService(repo)
	handler := NewTargetHandler(svc, discardLogger())
	mux := newRouter(handler)
	return chain(mux, RequestID, Logging(discardLogger()), Recovery(discardLogger()))
}

const validCreateBody = `{
	"name": "Example API",
	"url": "https://example.com/health",
	"method": "GET",
	"interval": "30s",
	"timeout": "5s",
	"expected_status_code": 200,
	"enabled": true
}`

func doRequest(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, r)
	return rr
}

func createTarget(t *testing.T, router http.Handler) targetResponse {
	t.Helper()
	rr := doRequest(t, router, http.MethodPost, "/api/v1/targets", validCreateBody)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var resp targetResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("create: failed to decode response: %v", err)
	}
	return resp
}

func TestTargetHandler_CreateSuccess(t *testing.T) {
	router := newTestRouter()
	rr := doRequest(t, router, http.MethodPost, "/api/v1/targets", validCreateBody)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp targetResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID == "" {
		t.Error("response did not include an ID")
	}
	if resp.Name != "Example API" || resp.Interval != "30s" || !resp.Enabled {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestTargetHandler_CreateInvalidRequest(t *testing.T) {
	router := newTestRouter()
	body := `{"name": "Example API", "url": "", "method": "GET", "interval": "30s", "timeout": "5s"}`

	rr := doRequest(t, router, http.MethodPost, "/api/v1/targets", body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
	var resp errorEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != codeInvalidRequest {
		t.Errorf("error code = %q, want %q", resp.Error.Code, codeInvalidRequest)
	}
}

func TestTargetHandler_CreateMalformedJSON(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodPost, "/api/v1/targets", `{not valid json`)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestTargetHandler_CreateInvalidDuration(t *testing.T) {
	router := newTestRouter()
	body := `{"name": "x", "url": "https://example.com", "method": "GET", "interval": "not-a-duration", "timeout": "5s"}`

	rr := doRequest(t, router, http.MethodPost, "/api/v1/targets", body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestTargetHandler_GetSuccess(t *testing.T) {
	router := newTestRouter()
	created := createTarget(t, router)

	rr := doRequest(t, router, http.MethodGet, "/api/v1/targets/"+created.ID, "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp targetResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != created.ID {
		t.Errorf("ID = %q, want %q", resp.ID, created.ID)
	}
}

func TestTargetHandler_GetMissing(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodGet, "/api/v1/targets/does-not-exist", "")

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
	var resp errorEnvelope
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Error.Code != codeNotFound {
		t.Errorf("error code = %q, want %q", resp.Error.Code, codeNotFound)
	}
}

func TestTargetHandler_List(t *testing.T) {
	router := newTestRouter()
	createTarget(t, router)
	createTarget(t, router)

	rr := doRequest(t, router, http.MethodGet, "/api/v1/targets", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp targetListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Targets) != 2 {
		t.Fatalf("len(Targets) = %d, want 2", len(resp.Targets))
	}
}

func TestTargetHandler_List_Empty(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodGet, "/api/v1/targets", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp targetListResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.Targets == nil {
		t.Error("Targets should be an empty array, not null, when there are no targets")
	}
}

func TestTargetHandler_UpdateSuccess(t *testing.T) {
	router := newTestRouter()
	created := createTarget(t, router)

	updateBody := `{
		"name": "Renamed",
		"url": "https://example.com/v2/health",
		"method": "POST",
		"interval": "1m",
		"timeout": "10s",
		"expected_status_code": 204,
		"enabled": false
	}`
	rr := doRequest(t, router, http.MethodPut, "/api/v1/targets/"+created.ID, updateBody)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var resp targetResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.ID != created.ID {
		t.Errorf("Update() changed the ID: got %q, want %q", resp.ID, created.ID)
	}
	if resp.Name != "Renamed" || resp.Enabled {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestTargetHandler_UpdateMissing(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodPut, "/api/v1/targets/does-not-exist", validCreateBody)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestTargetHandler_UpdateInvalidRequest(t *testing.T) {
	router := newTestRouter()
	created := createTarget(t, router)

	body := strings.Replace(validCreateBody, `"url": "https://example.com/health"`, `"url": "ftp://example.com"`, 1)
	rr := doRequest(t, router, http.MethodPut, "/api/v1/targets/"+created.ID, body)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusBadRequest, rr.Body.String())
	}
}

func TestTargetHandler_DeleteSuccess(t *testing.T) {
	router := newTestRouter()
	created := createTarget(t, router)

	rr := doRequest(t, router, http.MethodDelete, "/api/v1/targets/"+created.ID, "")
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if rr.Body.Len() != 0 {
		t.Errorf("204 response should have an empty body, got %q", rr.Body.String())
	}

	rr = doRequest(t, router, http.MethodGet, "/api/v1/targets/"+created.ID, "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("after delete, GET status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestTargetHandler_DeleteMissing(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodDelete, "/api/v1/targets/does-not-exist", "")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body = %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
}

func TestHealthHandler(t *testing.T) {
	router := newTestRouter()

	rr := doRequest(t, router, http.MethodGet, "/health", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}
