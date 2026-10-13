package health

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/app"
)

type checkFunc func(context.Context) error

func (f checkFunc) Ping(ctx context.Context) error { return f(ctx) }

func TestHealthAndReadinessTransitions(t *testing.T) {
	var draining atomic.Bool
	var available atomic.Bool
	var logOutput bytes.Buffer
	checker := checkFunc(func(context.Context) error {
		if !available.Load() {
			return errors.New("postgres://user:top-secret@host/db failed")
		}
		return nil
	})
	routes := Handler{checker, time.Second, &draining, app.NewLogger(&logOutput, slog.LevelInfo)}.Routes()

	assertStatus(t, routes, "/healthz", http.StatusOK)
	assertStatus(t, routes, "/readyz", http.StatusServiceUnavailable)
	available.Store(true)
	assertStatus(t, routes, "/readyz", http.StatusOK)
	available.Store(false)
	assertStatus(t, routes, "/healthz", http.StatusOK)
	assertStatus(t, routes, "/readyz", http.StatusServiceUnavailable)
	available.Store(true)
	assertStatus(t, routes, "/readyz", http.StatusOK)
	draining.Store(true)
	assertStatus(t, routes, "/readyz", http.StatusServiceUnavailable)
	assertStatus(t, routes, "/healthz", http.StatusOK)
	if strings.Contains(logOutput.String(), "top-secret") || strings.Contains(logOutput.String(), "postgres://") || strings.Contains(logOutput.String(), "DATABASE_URL") {
		t.Fatalf("log disclosed database details: %s", logOutput.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(logOutput.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("non-JSON readiness log: %v", err)
		}
		if entry["msg"] != "database readiness check failed" {
			t.Fatalf("unexpected readiness log: %v", entry)
		}
	}
}

func TestReadinessTimeoutAndResponsePrivacy(t *testing.T) {
	var draining atomic.Bool
	checker := checkFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	routes := Handler{checker, 20 * time.Millisecond, &draining, app.NewLogger(&bytes.Buffer{}, slog.LevelInfo)}.Routes()
	start := time.Now()
	recorder := httptest.NewRecorder()
	routes.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if recorder.Code != http.StatusServiceUnavailable || time.Since(start) > time.Second {
		t.Fatalf("readiness did not fail promptly: status %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "database") || strings.Contains(recorder.Body.String(), "postgres") {
		t.Fatalf("response disclosed database details: %s", recorder.Body.String())
	}
}

func TestShutdownDuringPingIsNotReady(t *testing.T) {
	var draining atomic.Bool
	checker := checkFunc(func(context.Context) error {
		draining.Store(true)
		return nil
	})
	routes := Handler{checker, time.Second, &draining, app.NewLogger(&bytes.Buffer{}, slog.LevelInfo)}.Routes()
	assertStatus(t, routes, "/readyz", http.StatusServiceUnavailable)
}

func assertStatus(t *testing.T, handler http.Handler, path string, want int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	if recorder.Code != want {
		t.Fatalf("%s: got %d, want %d", path, recorder.Code, want)
	}
	if strings.Contains(recorder.Body.String(), "top-secret") {
		t.Fatalf("%s disclosed a secret", path)
	}
}
