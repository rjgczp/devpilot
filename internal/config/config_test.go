package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testDSN = "postgres://devpilot:top-secret@127.0.0.1:5432/devpilot?sslmode=disable"

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadDoesNotReadDotEnvFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("DATABASE_URL="+testDSN+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	_, err = Load(os.Getenv)
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL: required") {
		t.Fatalf("expected missing environment variable, got %v", err)
	}
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{"DATABASE_URL": testDSN}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ReadinessTimeout != 2*time.Second || cfg.ShutdownTimeout != 10*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}

	cfg, err = Load(env(map[string]string{
		"DATABASE_URL":      testDSN,
		"HTTP_ADDR":         "127.0.0.1:9000",
		"LOG_LEVEL":         "debug",
		"READINESS_TIMEOUT": "500ms",
		"SHUTDOWN_TIMEOUT":  "3s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.LogLevel != slog.LevelDebug || cfg.ReadinessTimeout != 500*time.Millisecond || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected overrides: %+v", cfg)
	}
}

func TestLoadRejectsInvalidConfigurationWithoutSecrets(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		value  string
		wantIn string
	}{
		{"missing database URL", "DATABASE_URL", "", "DATABASE_URL"},
		{"invalid database URL", "DATABASE_URL", "postgres://devpilot:top-secret@", "DATABASE_URL"},
		{"invalid listen address", "HTTP_ADDR", "not-an-address", "HTTP_ADDR"},
		{"invalid port", "HTTP_ADDR", ":70000", "HTTP_ADDR"},
		{"invalid log level", "LOG_LEVEL", "trace", "LOG_LEVEL"},
		{"zero readiness timeout", "READINESS_TIMEOUT", "0s", "READINESS_TIMEOUT"},
		{"malformed readiness timeout", "READINESS_TIMEOUT", "later", "READINESS_TIMEOUT"},
		{"negative shutdown timeout", "SHUTDOWN_TIMEOUT", "-1s", "SHUTDOWN_TIMEOUT"},
		{"malformed shutdown timeout", "SHUTDOWN_TIMEOUT", "later", "SHUTDOWN_TIMEOUT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{"DATABASE_URL": testDSN}
			values[tt.key] = tt.value
			_, err := Load(env(values))
			if err == nil || !strings.Contains(err.Error(), tt.wantIn) {
				t.Fatalf("expected %s error, got %v", tt.wantIn, err)
			}
			if strings.Contains(err.Error(), "top-secret") || strings.Contains(err.Error(), testDSN) {
				t.Fatalf("configuration error exposed secret: %v", err)
			}
		})
	}
}
