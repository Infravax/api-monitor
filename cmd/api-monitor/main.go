// Command api-monitor is the entry point for the InfraVex API Monitor
// service. It only wires dependencies together and manages process
// startup/shutdown; all real behavior lives in the internal packages.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InfraVex/api-monitor/internal/api"
	"github.com/InfraVex/api-monitor/internal/config"
	"github.com/InfraVex/api-monitor/internal/storage"
	"github.com/InfraVex/api-monitor/internal/target"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("infravex api-monitor starting", "milestone", "M2")

	cfg := config.Load()
	logger.Info("configuration loaded", "config", cfg)

	repo := storage.NewMemoryTargetRepository()
	targetService := target.NewService(repo)
	targetHandler := api.NewTargetHandler(targetService, logger)

	server := api.NewServer(api.ServerConfig{
		Addr:         cfg.HTTPAddr,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}, targetHandler, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		logger.Error("http server failed", "error", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		return
	}
	logger.Info("http server stopped cleanly")
}
