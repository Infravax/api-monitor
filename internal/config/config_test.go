package config

import (
	"strings"
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
	if cfg.DatabaseURL != "postgres://postgres:postgres@localhost:5432/apimonitor?sslmode=disable" {
		t.Errorf("DatabaseURL = %q, want the local-dev default", cfg.DatabaseURL)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9090")
	t.Setenv("HTTP_READ_TIMEOUT", "1s")
	t.Setenv("HTTP_WRITE_TIMEOUT", "2s")
	t.Setenv("HTTP_IDLE_TIMEOUT", "3s")
	t.Setenv("WORKER_COUNT", "25")
	t.Setenv("QUEUE_SIZE", "250")
	t.Setenv("DATABASE_URL", "postgres://myuser:mypass@dbhost:5433/mydb?sslmode=require")

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
	if cfg.DatabaseURL != "postgres://myuser:mypass@dbhost:5433/mydb?sslmode=require" {
		t.Errorf("DatabaseURL = %q, want the overridden value", cfg.DatabaseURL)
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

func TestRedactDatabaseURL_HidesPassword(t *testing.T) {
	redacted := redactDatabaseURL("postgres://myuser:supersecret@dbhost:5432/mydb?sslmode=require")

	if strings.Contains(redacted, "supersecret") {
		t.Fatalf("redactDatabaseURL() = %q, password leaked", redacted)
	}
	for _, want := range []string{"myuser", "dbhost", "5432", "mydb", "sslmode=require"} {
		if !strings.Contains(redacted, want) {
			t.Errorf("redactDatabaseURL() = %q, want it to still contain %q", redacted, want)
		}
	}
}

func TestRedactDatabaseURL_NoPassword(t *testing.T) {
	redacted := redactDatabaseURL("postgres://myuser@dbhost:5432/mydb")
	if strings.Contains(redacted, "REDACTED") {
		t.Errorf("redactDatabaseURL() = %q, should not mask anything when there is no password", redacted)
	}
}

func TestRedactDatabaseURL_Invalid(t *testing.T) {
	// A control character makes url.Parse fail outright.
	redacted := redactDatabaseURL("postgres://\x7f")
	if redacted != "invalid" {
		t.Errorf("redactDatabaseURL() = %q, want %q for an unparsable value", redacted, "invalid")
	}
}

func TestConfig_LogValue_DoesNotLeakPassword(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://myuser:supersecret@dbhost:5432/mydb"}

	logged := cfg.LogValue().String()
	if strings.Contains(logged, "supersecret") {
		t.Fatalf("Config.LogValue() leaked the database password: %s", logged)
	}
}
