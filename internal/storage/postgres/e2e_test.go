package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InfraVex/api-monitor/internal/api"
	"github.com/InfraVex/api-monitor/internal/target"
)

// newTestServer wires the same stack main.go does (Repository -> Service
// -> Handler -> Server), but backed by the real PostgreSQL test database
// instead of the in-memory repository — proving the M2 REST contract
// still holds unchanged with PostgreSQL behind it, and that
// internal/api/internal/target needed no changes to support it.
func newTestServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	repo := NewTargetRepository(pool)
	svc := target.NewService(repo)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := api.NewTargetHandler(svc, logger)
	server := api.NewServer(api.ServerConfig{}, handler, logger)
	ts := httptest.NewServer(server.Handler)
	t.Cleanup(ts.Close)
	return ts
}

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := requireDatabase(t)

	if err := Migrate(dbURL); err != nil {
		t.Fatalf("Migrate() unexpected error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := NewPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := pool.Exec(context.Background(), "TRUNCATE TABLE targets"); err != nil {
		t.Fatalf("failed to truncate targets table before test: %v", err)
	}
	return pool
}

const e2eCreateBody = `{
	"name": "Example API",
	"url": "https://example.com/health",
	"method": "GET",
	"interval": "30s",
	"timeout": "5s",
	"expected_status_code": 200,
	"enabled": true
}`

func TestE2E_RESTCrudFlow_WithPostgres(t *testing.T) {
	pool := newTestPool(t)
	ts := newTestServer(t, pool)

	// Create
	resp, err := http.Post(ts.URL+"/api/v1/targets", "application/json", bytes.NewBufferString(e2eCreateBody))
	if err != nil {
		t.Fatalf("POST unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	resp.Body.Close()
	if created.ID == "" {
		t.Fatal("created target has no ID")
	}

	// Get
	resp, err = http.Get(ts.URL + "/api/v1/targets/" + created.ID)
	if err != nil {
		t.Fatalf("GET unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	// List
	resp, err = http.Get(ts.URL + "/api/v1/targets")
	if err != nil {
		t.Fatalf("GET list unexpected error: %v", err)
	}
	var list struct {
		Targets []struct{ ID string } `json:"targets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}
	resp.Body.Close()
	if len(list.Targets) != 1 {
		t.Fatalf("GET list returned %d targets, want 1", len(list.Targets))
	}

	// Update
	updateBody := `{"name":"Renamed","url":"https://example.com/v2","method":"GET","interval":"1m","timeout":"10s","expected_status_code":200,"enabled":false}`
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/v1/targets/"+created.ID, bytes.NewBufferString(updateBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	resp.Body.Close()

	// Delete
	req, err = http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/targets/"+created.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
	resp.Body.Close()

	resp, err = http.Get(ts.URL + "/api/v1/targets/" + created.ID)
	if err != nil {
		t.Fatalf("GET after delete unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after delete status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	resp.Body.Close()
}

// TestE2E_DataSurvivesRestart is the core M6 acceptance criterion: create
// a target, tear down every in-process object that held it (server,
// service, repository, connection pool — everything except the database
// itself), then rebuild the entire stack from scratch against the same
// database and confirm the target is still there. This is what actually
// proves persistence, as distinct from proving the repository's CRUD
// methods work in isolation (already covered by repository_test.go).
func TestE2E_DataSurvivesRestart(t *testing.T) {
	dbURL := requireDatabase(t)
	if err := Migrate(dbURL); err != nil {
		t.Fatalf("Migrate() unexpected error: %v", err)
	}

	// "Instance 1": create a target, then close everything.
	firstCtx, firstCancel := context.WithTimeout(context.Background(), 5*time.Second)
	firstPool, err := NewPool(firstCtx, dbURL)
	firstCancel()
	if err != nil {
		t.Fatalf("NewPool() unexpected error: %v", err)
	}
	if _, err := firstPool.Exec(context.Background(), "TRUNCATE TABLE targets"); err != nil {
		t.Fatalf("failed to truncate targets table before test: %v", err)
	}

	ts1 := newTestServer(t, firstPool)
	resp, err := http.Post(ts1.URL+"/api/v1/targets", "application/json", bytes.NewBufferString(e2eCreateBody))
	if err != nil {
		t.Fatalf("POST unexpected error: %v", err)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}
	resp.Body.Close()

	ts1.Close()       // "stop the API"
	firstPool.Close() // "close the connection pool" — everything in-process is now gone

	// "Instance 2": brand new pool, repository, service, handler, server —
	// simulating the process having fully restarted — against the same
	// database.
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 5*time.Second)
	secondPool, err := NewPool(secondCtx, dbURL)
	secondCancel()
	if err != nil {
		t.Fatalf("NewPool() unexpected error on 'restart': %v", err)
	}
	t.Cleanup(secondPool.Close)

	ts2 := newTestServer(t, secondPool)
	resp, err = http.Get(ts2.URL + "/api/v1/targets/" + created.ID)
	if err != nil {
		t.Fatalf("GET after 'restart' unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET after 'restart' status = %d, want %d — target did not survive restart", resp.StatusCode, http.StatusOK)
	}
	var got struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.Name != "Example API" {
		t.Errorf("Name = %q after restart, want %q", got.Name, "Example API")
	}
}
