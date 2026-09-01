package api

import "net/http"

// Health handles GET /health. It reports only that the process is alive
// and able to serve HTTP — a liveness check, not a readiness check. There
// is nothing to be "not ready" for yet: no database or other dependency
// exists in this milestone (see docs/architecture.md).
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
