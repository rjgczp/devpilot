//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture

package integration

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/ai"
	"github.com/rjgczp/devpilot/internal/app"
	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// Fixture-only real API. No new production role, migration or execution route.
func webDemoServer(t *testing.T, db *pgxpool.Pool, m monitor.Public) func() {
	t.Helper()
	port := os.Getenv("DEVPILOT_DEMO_PORT")
	if port == "" {
		port = "5174"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1024 || n > 65535 {
		t.Fatal("invalid demo port")
	}
	origin, err := config.ParseOrigin("http://127.0.0.1:" + port)
	if err != nil {
		t.Fatal(err)
	}
	policy := config.AuthConfig{Mode: "development", Origin: origin, SessionTTL: time.Hour}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	denied := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(403)
		_, _ = io.WriteString(w, `{"error":{"code":"isolated_demo_readonly"}}`)
	})
	authentication := auth.New(user.Store{Pool: db}, session.Store{Pool: db}, password.New(), session.NewTokens(), password.NewBudget(), policy, logger)
	cfg, err := config.Load(func(key string) string {
		switch key {
		case "DATABASE_URL":
			return os.Getenv("TEST_DATABASE_URL")
		case "AUTH_MODE":
			return "development"
		case "AUTH_ORIGIN":
			return "http://127.0.0.1:" + port
		case "HTTP_ADDR":
			return "127.0.0.1:" + port
		}
		return os.Getenv(key)
	})
	if err != nil {
		t.Fatal("invalid server-only demo configuration")
	}
	routes := app.NewRoutes(denied, denied, authentication, workspace.New(workspace.Store{Pool: db}, policy, logger), project.New(project.Store{Pool: db}, policy, logger), monitor.New(monitor.Store{Pool: db}, policy, logger), ai.New(monitor.Store{Pool: db}, policy, cfg.AI))
	base := status75Base(m)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow only actual login/logout and existing finite Owner intent APIs for
		// this fixed Monitor. Config creation/editing and other identities cannot
		// introduce another URL/version into this demo's target catalog.
		if r.Method != "GET" && r.Method != "HEAD" && !(r.Method == "POST" && (r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/logout" || r.URL.Path == "/api/v1/ai/diagnose")) && !(r.Method == "PUT" && (r.URL.Path == base+"/activation" || r.URL.Path == base+"/execution-authorization")) {
			denied.ServeHTTP(w, r)
			return
		}
		routes.ServeHTTP(w, r)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:5174")
	if err != nil {
		t.Fatal(err)
	}
	server := app.NewServer("127.0.0.1:5174", handler)
	go func() { _ = server.Serve(listener) }()
	return func() { _ = server.Close() }
}
