// Package config loads application configuration from environment
// variables, with sensible defaults so the application runs unconfigured.
// It deliberately does not use a configuration framework: at this size,
// os.Getenv plus a handful of defaults is easier to read and debug than a
// framework would be.
package config

import (
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config holds the application's runtime configuration.
type Config struct {
	HTTPAddr         string
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
	// WorkerCount and QueueSize configure the M5 worker pool. See
	// worker.Config's doc comments for why these defaults were chosen
	// (10 workers: conservative for an I/O-bound workload hitting
	// third-party APIs; 100 queue slots: enough to absorb a burst
	// without an unbounded queue).
	WorkerCount int
	QueueSize   int
	// DatabaseURL is a PostgreSQL connection string
	// (postgres://user:password@host:port/dbname?...). It defaults to a
	// well-known local-development convention (postgres/postgres on
	// localhost:5432) purely for zero-config `go run` — the same
	// "sensible default so the app runs unconfigured" philosophy as
	// every other setting here — not something to deploy as-is. See
	// LogValue: this must never be logged unredacted, since it typically
	// carries a real credential.
	DatabaseURL string
}

// Load builds a Config from environment variables, falling back to
// defaults for anything unset or unparsable:
//
//	HTTP_ADDR=:8080
//	HTTP_READ_TIMEOUT=5s
//	HTTP_WRITE_TIMEOUT=10s
//	HTTP_IDLE_TIMEOUT=60s
//	WORKER_COUNT=10
//	QUEUE_SIZE=100
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable
func Load() Config {
	return Config{
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		HTTPReadTimeout:  getEnvDuration("HTTP_READ_TIMEOUT", 5*time.Second),
		HTTPWriteTimeout: getEnvDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout:  getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
		WorkerCount:      getEnvInt("WORKER_COUNT", 10),
		QueueSize:        getEnvInt("QUEUE_SIZE", 100),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable"),
	}
}

// LogValue lets Config be logged directly via slog without manually
// listing each field at every call site. DatabaseURL is redacted before
// logging (see redactDatabaseURL) — it routinely carries a real password,
// and this is the one config value in this struct where logging it
// verbatim would leak a credential.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("http_addr", c.HTTPAddr),
		slog.Duration("http_read_timeout", c.HTTPReadTimeout),
		slog.Duration("http_write_timeout", c.HTTPWriteTimeout),
		slog.Duration("http_idle_timeout", c.HTTPIdleTimeout),
		slog.Int("worker_count", c.WorkerCount),
		slog.Int("queue_size", c.QueueSize),
		slog.String("database_url", redactDatabaseURL(c.DatabaseURL)),
	)
}

// redactDatabaseURL replaces a connection string's password (if any) with
// "***", leaving the rest (scheme, user, host, port, dbname, query)
// intact for debugging. If the value doesn't parse as a URL at all, it is
// not returned verbatim — "invalid" is logged instead, so a malformed
// value can never accidentally leak whatever it actually contains.
func redactDatabaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid"
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

// getEnvInt parses key as a positive integer, falling back to fallback if
// unset, unparsable, or not positive — the same silent-fallback
// philosophy as getEnvDuration, so a misconfigured environment variable
// degrades to a safe default instead of crashing the application.
func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
