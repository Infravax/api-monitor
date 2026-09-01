// Command api-monitor is the entry point for the InfraVex API Monitor service.
//
// At this stage (Milestone 0) the binary only proves that the module builds,
// starts, and shuts down cleanly. It intentionally contains no monitoring
// logic yet — that arrives in later milestones as the internal packages are
// implemented and wired in here.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("infravex api-monitor starting", "milestone", "M1")

	<-ctx.Done()

	logger.Info("shutdown signal received, exiting")
}
