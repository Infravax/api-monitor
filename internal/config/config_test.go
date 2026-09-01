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
	t.Setenv("WORKER_COUNT", "25")
	t.Setenv("QUEUE_SIZE", "250")

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
	if cfg.WorkerCount != 25 {
		t.Errorf("WorkerCount = %d, want 25", cfg.WorkerCount)
	}
	if cfg.QueueSize != 250 {
		t.Errorf("QueueSize = %d, want 250", cfg.QueueSize)
	}
}

func TestLoad_InvalidDurationFallsBackToDefault(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")

	cfg := Load()

	if cfg.HTTPReadTimeout != 5*time.Second {
		t.Errorf("HTTPReadTimeout = %v, want default 5s for invalid input", cfg.HTTPReadTimeout)
	}
}

func TestLoad_InvalidIntFallsBackToDefault(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"non-numeric worker count", map[string]string{"WORKER_COUNT": "not-a-number"}},
		{"zero worker count", map[string]string{"WORKER_COUNT": "0"}},
		{"negative worker count", map[string]string{"WORKER_COUNT": "-5"}},
		{"non-numeric queue size", map[string]string{"QUEUE_SIZE": "not-a-number"}},
		{"zero queue size", map[string]string{"QUEUE_SIZE": "0"}},
		{"negative queue size", map[string]string{"QUEUE_SIZE": "-5"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			cfg := Load()

			if cfg.WorkerCount != 10 {
				t.Errorf("WorkerCount = %d, want default 10", cfg.WorkerCount)
			}
			if cfg.QueueSize != 100 {
				t.Errorf("QueueSize = %d, want default 100", cfg.QueueSize)
			}
		})
	}
}
