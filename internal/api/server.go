package api

import (
	"log/slog"
	"net/http"
	"time"
)

// ServerConfig holds the settings NewServer needs. It is a small,
// api-owned struct rather than a direct dependency on internal/config, so
// this package doesn't need to know how configuration is loaded (env
// vars, flags, etc.) — main.go does that translation.
type ServerConfig struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// NewServer builds an *http.Server with routes registered and the
// request-ID/logging/recovery middleware applied.
func NewServer(cfg ServerConfig, targetHandler *TargetHandler, logger *slog.Logger) *http.Server {
	mux := newRouter(targetHandler)
	handler := chain(mux, RequestID, Logging(logger), Recovery(logger))

	return &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}
}
