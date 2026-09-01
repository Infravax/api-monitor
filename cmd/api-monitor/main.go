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
	"github.com/InfraVex/api-monitor/internal/worker"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("infravex api-monitor starting", "milestone", "M5")

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

	pool := worker.New(worker.Config{
		Checker:     checker.NewChecker(nil),
		WorkerCount: cfg.WorkerCount,
		QueueSize:   cfg.QueueSize,
		Logger:      logger,
	})

	// The Scheduler's Checker is now the worker pool, not checker.Checker
	// directly — *worker.Pool satisfies the exact same TargetChecker
	// interface (Check(ctx, target.Target) checker.CheckResult), so
	// internal/scheduler required no code changes at all for M5. The
	// Scheduler still decides *when* a target is due; actual check
	// execution now runs on a pool worker, bounded by WorkerCount, instead
	// of directly in the Scheduler's own per-target goroutine.
	//
	// OnResult is left unset: nothing yet consumes a CheckResult beyond
	// the pool's own lifecycle logging (M6 will persist results, M7/M8
	// will react to them). This is the extension point those milestones
	// will use.
	sched := scheduler.New(scheduler.Config{
		Targets: targetService,
		Checker: pool,
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

	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		if err := pool.Start(ctx); err != nil {
			logger.Error("worker pool stopped with error", "error", err)
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

	// The scheduler and pool both stop via the same ctx (already canceled
	// by this point via signal.NotifyContext, or will be via stop() on
	// return). Wait for the scheduler first — it's the one calling
	// pool.Check — then the pool, so no goroutine outlives main.
	stop()
	<-schedulerDone
	logger.Info("scheduler stopped cleanly")
	<-poolDone
	logger.Info("worker pool stopped cleanly")
}
