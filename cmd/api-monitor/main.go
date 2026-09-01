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
	"github.com/InfraVex/api-monitor/internal/storage/postgres"
	"github.com/InfraVex/api-monitor/internal/target"
	"github.com/InfraVex/api-monitor/internal/worker"
)

// dbConnectTimeout bounds how long startup waits for PostgreSQL to become
// reachable before giving up. This is a startup-only concern — once
// connected, the pool's own settings (internal/storage/postgres) govern
// individual query timeouts, not this constant.
const dbConnectTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("infravex api-monitor starting", "milestone", "M6")

	cfg := config.Load()
	logger.Info("configuration loaded", "config", cfg)

	// PostgreSQL is the first genuinely required piece of external
	// infrastructure this application has (see docs/architecture.md): if
	// it can't be reached, or migrations can't run, that must stop
	// startup here — loudly, with a non-zero exit — rather than let the
	// application come up and silently fail every request that touches
	// storage. Every other dependency constructed by this function is
	// in-process and cannot fail this way.
	connectCtx, cancel := context.WithTimeout(context.Background(), dbConnectTimeout)
	pool, err := postgres.NewPool(connectCtx, cfg.DatabaseURL)
	cancel()
	if err != nil {
		logger.Error("failed to connect to postgresql", "error", err)
		os.Exit(1)
	}

	if err := postgres.Migrate(cfg.DatabaseURL); err != nil {
		logger.Error("failed to run database migrations", "error", err)
		pool.Close()
		os.Exit(1)
	}
	logger.Info("database connected and migrations applied")

	repo := postgres.NewTargetRepository(pool)
	targetService := target.NewService(repo)
	targetHandler := api.NewTargetHandler(targetService, logger)

	server := api.NewServer(api.ServerConfig{
		Addr:         cfg.HTTPAddr,
		ReadTimeout:  cfg.HTTPReadTimeout,
		WriteTimeout: cfg.HTTPWriteTimeout,
		IdleTimeout:  cfg.HTTPIdleTimeout,
	}, targetHandler, logger)

	workerPool := worker.New(worker.Config{
		Checker:     checker.NewChecker(nil),
		WorkerCount: cfg.WorkerCount,
		QueueSize:   cfg.QueueSize,
		Logger:      logger,
	})

	// OnResult is left unset: nothing yet consumes a CheckResult beyond
	// the pool's own lifecycle logging (M7/M8 will react to them; M6
	// only persists Targets, not CheckResults, since the Scheduler and
	// worker pool don't read or write the database at all — the only
	// database consumer in this process is targetService, reached
	// through the REST API). This is the extension point those future
	// milestones will use.
	sched := scheduler.New(scheduler.Config{
		Targets: targetService,
		Checker: workerPool,
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
		if err := workerPool.Start(ctx); err != nil {
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
	// pool.Check — then the pool, then finally close the database pool:
	// targetService (reached only through the HTTP server, already
	// stopped, and through the scheduler, now also stopped) is the only
	// consumer of it, so nothing can still be using it by this point.
	stop()
	<-schedulerDone
	logger.Info("scheduler stopped cleanly")
	<-poolDone
	logger.Info("worker pool stopped cleanly")
	pool.Close()
	logger.Info("database connection pool closed")
}
