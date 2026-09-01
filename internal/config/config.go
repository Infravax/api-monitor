// Package config loads application configuration from environment
// variables, with sensible defaults so the application runs unconfigured.
// It deliberately does not use a configuration framework: at this size,
// os.Getenv plus a handful of defaults is easier to read and debug than a
// framework would be.
package config

import (
	"log/slog"
	"os"
	"time"
)

// Config holds the application's runtime configuration.
type Config struct {
	HTTPAddr         string
	HTTPReadTimeout  time.Duration
	HTTPWriteTimeout time.Duration
	HTTPIdleTimeout  time.Duration
}

// Load builds a Config from environment variables, falling back to
// defaults for anything unset or unparsable:
//
//	HTTP_ADDR=:8080
//	HTTP_READ_TIMEOUT=5s
//	HTTP_WRITE_TIMEOUT=10s
//	HTTP_IDLE_TIMEOUT=60s
func Load() Config {
	return Config{
		HTTPAddr:         getEnv("HTTP_ADDR", ":8080"),
		HTTPReadTimeout:  getEnvDuration("HTTP_READ_TIMEOUT", 5*time.Second),
		HTTPWriteTimeout: getEnvDuration("HTTP_WRITE_TIMEOUT", 10*time.Second),
		HTTPIdleTimeout:  getEnvDuration("HTTP_IDLE_TIMEOUT", 60*time.Second),
	}
}

// LogValue lets Config be logged directly via slog without manually
// listing each field at every call site.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("http_addr", c.HTTPAddr),
		slog.Duration("http_read_timeout", c.HTTPReadTimeout),
		slog.Duration("http_write_timeout", c.HTTPWriteTimeout),
		slog.Duration("http_idle_timeout", c.HTTPIdleTimeout),
	)
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
