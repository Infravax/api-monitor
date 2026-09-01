package api

import "net/http"

// newRouter registers all routes. Go 1.22+'s http.ServeMux supports
// method-specific patterns and {path} parameters natively, so no
// third-party router is needed for routes this simple.
func newRouter(targetHandler *TargetHandler) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", Health)

	mux.HandleFunc("POST /api/v1/targets", targetHandler.Create)
	mux.HandleFunc("GET /api/v1/targets", targetHandler.List)
	mux.HandleFunc("GET /api/v1/targets/{id}", targetHandler.Get)
	mux.HandleFunc("PUT /api/v1/targets/{id}", targetHandler.Update)
	mux.HandleFunc("DELETE /api/v1/targets/{id}", targetHandler.Delete)

	return mux
}
