package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.HTTPReadTimeout != 5*time.Second {
		t.Errorf("HTTPReadTimeout = %v, want 5s", cfg.HTTPReadTimeout)
	}
	if cfg.HTTPWriteTimeout != 10*time.Second {
		t.Errorf("HTTPWriteTimeout = %v, want 10s", cfg.HTTPWriteTimeout)
	}
	if cfg.HTTPIdleTimeout != 60*time.Second {
		t.Errorf("HTTPIdleTimeout = %v, want 60s", cfg.HTTPIdleTimeout)
	}
	if cfg.WorkerCount != 10 {
		t.Errorf("WorkerCount = %d, want 10", cfg.WorkerCount)
	}
	if cfg.QueueSize != 100 {
		t.Errorf("QueueSize = %d, want 100", cfg.QueueSize)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("HTTP_READ_TIMEOUT", "1s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "2s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "3s")

	cfg := Load()

	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.HTTPReadTimeout != time.Second {
		t.Errorf("HTTPReadTimeout = %v, want 1s", cfg.HTTPReadTimeout)
	}
	if cfg.HTTPWriteTimeout != 2*time.Second {
		t.Errorf("HTTPWriteTimeout = %v, want 2s", cfg.HTTPWriteTimeout)
	}
	if cfg.HTTPIdleTimeout != 3*time.Second {
		t.Errorf("HTTPIdleTimeout = %v, want 3s", cfg.HTTPIdleTimeout)
	}
}

func TestLoad_InvalidDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")

	cfg := Load()

	if cfg.HTTPReadTimeout != 5*time.Second {
		t.Errorf("HTTPReadTimeout = %v, want default 5s for invalid input", cfg.HTTPReadTimeout)
	}
}
