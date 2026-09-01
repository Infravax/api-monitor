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
	"github.com/InfraVex/api-monitor/internal/checker"
	"github.com/InfraVex/api-monitor/internal/config"
	"github.com/InfraVex/api-monitor/internal/scheduler"
	"github.com/InfraVex/api-monitor/internal/storage"
	"github.com/InfraVex/api-monitor/internal/target"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("infravex api-monitor starting", "milestone", "M4")

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

	// OnResult is left unset: nothing yet consumes a CheckResult beyond
	// the scheduler's own lifecycle logging (M6 will persist results,
	// M7/M8 will react to them). This is the extension point those
	// milestones will use.
	sched := scheduler.New(scheduler.Config{
		Targets: targetService,
		Checker: checker.NewChecker(nil),
		Logger:  logger,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	schedulerDone := make(chan struct{})
	go func() {
		defer close(schedulerDone)
		if err := sched.Start(ctx); err != nil {
			logger.Error("scheduler stopped with error", "error", err)
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
	} else {
		logger.Info("http server stopped cleanly")
	}

	// The scheduler stops via the same ctx (already canceled by this
	// point via signal.NotifyContext, or will be via stop() on return);
	// wait for it to actually finish so no goroutine outlives main.
	stop()
	<-schedulerDone
	logger.Info("scheduler stopped cleanly")
}
