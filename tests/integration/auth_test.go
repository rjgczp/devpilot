//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/app"
	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/health"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/registration"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/testdb"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func authServer(t *testing.T, db *pgxpool.Pool, tls bool, logs *bytes.Buffer) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewUnstartedServer(http.NotFoundHandler())
	scheme, mode := "http", "development"
	if tls {
		scheme, mode = "https", "production"
	}
	origin, err := config.ParseOrigin(scheme + "://" + server.Listener.Addr().String())
	if err != nil {
		t.Fatal("origin fixture")
	}
	logger := app.NewLogger(logs, slog.LevelInfo)
	var draining atomic.Bool
	budget := password.NewBudget()
	passwords := password.New()
	users := user.Store{Pool: db}
	policy := config.AuthConfig{Mode: mode, Origin: origin, SessionTTL: 24 * time.Hour}
	server.Config.Handler = app.NewRoutes(health.Handler{Checker: db, Timeout: time.Second, Draining: &draining, Logger: logger}.Routes(), registration.New(users, passwords, logger, budget), auth.New(users, session.Store{Pool: db}, passwords, session.NewTokens(), budget, policy, logger), workspace.New(workspace.Store{Pool: db}, policy, logger))
	if tls {
		server.StartTLS()
	} else {
		server.Start()
	}
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 8 * time.Second
	client.Jar, err = cookiejar.New(nil)
	if err != nil {
		t.Fatal("Cookie jar fixture")
	}
	return server, client
}

func authRequest(t *testing.T, client *http.Client, base, method, path, payload, origin string) (int, http.Header, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, base+"/api/v1/auth/"+path, strings.NewReader(payload))
	if err != nil {
		t.Fatal("request fixture")
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		r.Header.Set("X-DevPilot-CSRF", "1")
	}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal("auth HTTP request failed")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("response read")
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("auth safe headers")
	}
	return resp.StatusCode, resp.Header, b
}
func loginBody(email, secret string) string {
	b, _ := json.Marshal(map[string]string{"email": email, "password": secret})
	return string(b)
}
func requireCode(t *testing.T, b []byte, want string) {
	t.Helper()
	var out struct{ Error struct{ Code string } }
	if json.Unmarshal(b, &out) != nil || out.Error.Code != want {
		t.Fatal("auth error contract")
	}
}

func TestAuthenticationWithPostgreSQL(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "loopback-http", true: "production-https"}[tls], func(t *testing.T) {
			ctx := context.Background()
			var logs bytes.Buffer
			server, client := authServer(t, db, tls, &logs)
			email := "http-browser@example.com"
			if tls {
				email = "https-browser@example.com"
			}
			status, registered := post(t, client, server.URL, email, testPassword)
			if status != 201 {
				t.Fatal("register prerequisite")
			}
			var u user.Public
			if json.Unmarshal(registered["data"], &u) != nil {
				t.Fatal("registration DTO")
			}
			var hash string
			var initialCreated, initialUpdated time.Time
			if db.QueryRow(ctx, `SELECT password_hash,created_at,updated_at FROM users WHERE id=$1`, u.ID).Scan(&hash, &initialCreated, &initialUpdated) != nil {
				t.Fatal("user snapshot")
			}
			status, headers, b := authRequest(t, client, server.URL, "POST", "login", loginBody(strings.ToUpper(email), testPassword), server.URL)
			if status != 200 {
				t.Fatal("login failed")
			}
			cookie := (&http.Response{Header: headers}).Cookies()[0]
			if cookie.HttpOnly != true || cookie.Secure != tls || cookie.Path != "/" || cookie.Domain != "" || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("real Cookie contract")
			}
			digest, err := session.HashToken(cookie.Value)
			if err != nil {
				t.Fatal("issued token format")
			}
			var created, expires time.Time
			var stored []byte
			if db.QueryRow(ctx, `SELECT created_at,expires_at,token_hash FROM sessions WHERE token_hash=$1`, digest[:]).Scan(&created, &expires, &stored) != nil || expires.Sub(created) != 24*time.Hour || !bytes.Equal(stored, digest[:]) || bytes.Contains(b, []byte(cookie.Value)) {
				t.Fatal("digest/default lifetime/projection")
			}
			for i := 0; i < 2; i++ {
				status, h, b := authRequest(t, client, server.URL, "GET", "me", "", "")
				if status != 200 || h.Get("Set-Cookie") != "" || !bytes.Contains(b, []byte(u.ID)) {
					t.Fatal("Cookie jar identity")
				}
			}
			if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1 AND created_at=$2 AND expires_at=$3`, digest[:], created, expires) != 1 || queryInt(t, db, `SELECT count(*) FROM users WHERE id=$1 AND password_hash=$2 AND created_at=$3 AND updated_at=$4 AND email_verified_at IS NULL`, u.ID, hash, initialCreated, initialUpdated) != 1 {
				t.Fatal("login/me changed state")
			}
			status, secondHeaders, _ := authRequest(t, client, server.URL, "POST", "login", loginBody(email, testPassword), server.URL)
			if status != 200 {
				t.Fatal("second login")
			}
			secondCookie := (&http.Response{Header: secondHeaders}).Cookies()[0]
			secondDigest, err := session.HashToken(secondCookie.Value)
			if err != nil || secondCookie.Value == cookie.Value {
				t.Fatal("session independence")
			}
			if _, err := (session.Store{Pool: db}).Lookup(ctx, digest); err != nil {
				t.Fatal("relogin revoked prior session")
			}
			// Login CSRF must be checked even when the browser is already logged in.
			before := queryInt(t, db, `SELECT count(*) FROM sessions`)
			for _, path := range []string{"login", "logout"} {
				status, h, b := authRequest(t, client, server.URL, "POST", path, loginBody(email, testPassword), "https://attacker.example")
				if status != 403 || h.Get("Set-Cookie") != "" {
					t.Fatal("forged mutation")
				}
				requireCode(t, b, "csrf_rejected")
			}
			if queryInt(t, db, `SELECT count(*) FROM sessions`) != before {
				t.Fatal("CSRF wrote session")
			}
			var rejection []byte
			for _, kind := range []string{"missing", "wrong", "disabled", "bad-PHC"} {
				attemptEmail, attemptPassword := email, "incorrect"
				switch kind {
				case "missing":
					attemptEmail = "not-found@example.com"
					attemptPassword = "devpilot dummy credential"
				case "disabled":
					if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, u.ID); err != nil {
						t.Fatal("disable fixture")
					}
					attemptPassword = testPassword
				case "bad-PHC":
					if _, err := db.Exec(ctx, `UPDATE users SET status='active',password_hash='malformed-PHC' WHERE id=$1`, u.ID); err != nil {
						t.Fatal("PHC fixture")
					}
				}
				status, h, b := authRequest(t, client, server.URL, "POST", "login", loginBody(attemptEmail, attemptPassword), server.URL)
				if status != 401 || h.Get("Set-Cookie") != "" {
					t.Fatal("credential failure contract")
				}
				requireCode(t, b, "invalid_credentials")
				if rejection == nil {
					rejection = b
				} else if !bytes.Equal(rejection, b) {
					t.Fatal("account enumeration response")
				}
				if kind == "disabled" {
					status, _, b := authRequest(t, client, server.URL, "GET", "me", "", "")
					if status != 401 {
						t.Fatal("disabled session")
					}
					requireCode(t, b, "unauthenticated")
				}
			}
			if _, err := db.Exec(ctx, `UPDATE users SET status='active',password_hash=$2 WHERE id=$1`, u.ID, password.DummyPHC); err != nil {
				t.Fatal("changed password fixture")
			}
			status, _, b = authRequest(t, client, server.URL, "POST", "login", loginBody(email, testPassword), server.URL)
			if status != 401 {
				t.Fatal("changed password accepted old password")
			}
			requireCode(t, b, "invalid_credentials")
			if _, err := db.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, hash); err != nil {
				t.Fatal("restore credential")
			}
			// Expiry enforced with the row retained and no cleanup worker present.
			if _, err := db.Exec(ctx, `UPDATE sessions SET created_at=statement_timestamp()-interval '2h',expires_at=statement_timestamp()-interval '1h' WHERE token_hash=$1`, secondDigest[:]); err != nil {
				t.Fatal("expiry fixture")
			}
			status, h, b := authRequest(t, client, server.URL, "GET", "me", "", "")
			if status != 401 || h.Get("Set-Cookie") != "" || queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, secondDigest[:]) != 1 {
				t.Fatal("retained expiry")
			}
			requireCode(t, b, "unauthenticated")
			status, h, b = authRequest(t, client, server.URL, "POST", "logout", "", server.URL)
			if status != 204 || len(b) != 0 || h.Get("Set-Cookie") == "" {
				t.Fatal("expired logout")
			}
			// Revoking one independent session never revokes the previously issued one.
			if _, err := (session.Store{Pool: db}).Lookup(ctx, digest); err != nil {
				t.Fatal("logout revoked other token")
			}
			status, thirdHeaders, _ := authRequest(t, client, server.URL, "POST", "login", loginBody(email, testPassword), server.URL)
			if status != 200 {
				t.Fatal("login after logout")
			}
			thirdCookie := (&http.Response{Header: thirdHeaders}).Cookies()[0]
			thirdDigest, _ := session.HashToken(thirdCookie.Value)
			status, _, b = authRequest(t, client, server.URL, "POST", "logout", "", server.URL)
			if status != 204 || len(b) != 0 {
				t.Fatal("logout")
			}
			// Replay bypasses Cookie jar deletion to prove server-side revocation.
			r, _ := http.NewRequest("GET", server.URL+"/api/v1/auth/me", nil)
			r.AddCookie(thirdCookie)
			resp, err := client.Do(r)
			if err != nil {
				t.Fatal("replay HTTP")
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Fatal("old token replay authenticated")
			}
			if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1 AND revoked_at IS NOT NULL`, thirdDigest[:]) != 1 {
				t.Fatal("logout did not revoke retained row")
			}
			status, _, b = authRequest(t, client, server.URL, "POST", "logout", "", server.URL)
			if status != 204 || len(b) != 0 {
				t.Fatal("idempotent logout")
			}
			if strings.Contains(logs.String(), testPassword) || strings.Contains(logs.String(), hash) || strings.Contains(logs.String(), cookie.Value) || strings.Contains(logs.String(), "example.com") {
				t.Fatal("credential/session diagnostic disclosure")
			}
		})
	}
}

func TestAuthenticationDatabaseFailuresAndCancellation(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	var logs bytes.Buffer
	server, client := authServer(t, db, false, &logs)
	if status, _ := post(t, client, server.URL, "failures@example.com", testPassword); status != 201 {
		t.Fatal("user fixture")
	}
	status, _, _ := authRequest(t, client, server.URL, "POST", "login", loginBody("failures@example.com", testPassword), server.URL)
	if status != 200 {
		t.Fatal("session fixture")
	}
	ctx := context.Background()
	if _, err := db.Exec(ctx, `ALTER TABLE sessions RENAME TO sessions_hidden`); err != nil {
		t.Fatal("schema fixture")
	}
	for _, path := range []string{"login", "me", "logout"} {
		method := "POST"
		if path == "me" {
			method = "GET"
		}
		status, h, b := authRequest(t, client, server.URL, method, path, loginBody("failures@example.com", testPassword), server.URL)
		if status != 500 || h.Get("Set-Cookie") != "" {
			t.Fatal("missing sessions error")
		}
		requireCode(t, b, "internal_error")
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal("health with missing schema")
		}
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatal("health schema semantics changed")
		}
	}
	// Session-specific schema failure must not break registration.
	if status, _ := post(t, client, server.URL, "registration-still-works@example.com", testPassword); status != 201 {
		t.Fatal("session fault poisoned registration")
	}
	if _, err := db.Exec(ctx, `ALTER TABLE sessions_hidden RENAME TO sessions`); err != nil {
		t.Fatal("restore schema")
	}
	for _, path := range []string{"login", "me", "logout"} {
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal("timeout fixture")
		}
		if _, err := tx.Exec(ctx, `LOCK TABLE sessions IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal("session lock")
		}
		method := "POST"
		if path == "me" {
			method = "GET"
		}
		status, h, b := authRequest(t, client, server.URL, method, path, loginBody("failures@example.com", testPassword), server.URL)
		tx.Rollback(ctx)
		if status != 503 || h.Get("Set-Cookie") != "" {
			t.Fatal("session deadline")
		}
		requireCode(t, b, "database_timeout")
	}
	// Real socket cancellation propagates into a blocked PostgreSQL lookup.
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal("cancel fixture")
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE sessions IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal("cancel lock")
	}
	requestContext, cancel := context.WithCancel(ctx)
	defer cancel()
	r, _ := http.NewRequestWithContext(requestContext, "GET", server.URL+"/api/v1/auth/me", nil)
	done := make(chan error, 1)
	go func() {
		resp, err := client.Do(r)
		if resp != nil {
			resp.Body.Close()
		}
		done <- err
	}()
	deadline := time.Now().Add(time.Second)
	for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM sessions s JOIN users%'`) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("identity request did not block")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if <-done == nil {
		t.Fatal("disconnected request received success")
	}
	for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM sessions s JOIN users%'`) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("identity cancellation not propagated")
		}
		time.Sleep(time.Millisecond)
	}
	tx.Rollback(ctx)
	status, _, _ = authRequest(t, client, server.URL, "GET", "me", "", "")
	if status != 200 {
		t.Fatal("same-process recovery")
	}
	// Unreachable connections are dependency failure, not missing identity.
	badConfig, err := pgxpool.ParseConfig("postgres://test:sentinel@127.0.0.1:1/" + testdb.Name + "?sslmode=disable")
	if err != nil {
		t.Fatal("unavailable fixture")
	}
	badConfig.ConnConfig.ConnectTimeout = 100 * time.Millisecond
	bad, err := pgxpool.NewWithConfig(ctx, badConfig)
	if err != nil {
		t.Fatal("unavailable pool")
	}
	defer bad.Close()
	var badLogs bytes.Buffer
	badServer, badClient := authServer(t, bad, false, &badLogs)
	// Give the new host's jar a parseable token so me/logout actually query DB.
	secret, _, _ := session.NewTokens().New()
	for _, path := range []string{"login", "me", "logout"} {
		method := "POST"
		if path == "me" {
			method = "GET"
		}
		r, _ := http.NewRequest(method, badServer.URL+"/api/v1/auth/"+path, strings.NewReader(loginBody("failures@example.com", testPassword)))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", badServer.URL)
		r.Header.Set("X-DevPilot-CSRF", "1")
		r.AddCookie(&http.Cookie{Name: "devpilot_session", Value: secret})
		resp, err := badClient.Do(r)
		if err != nil {
			t.Fatal("unavailable HTTP")
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 503 || resp.Header.Get("Set-Cookie") != "" {
			t.Fatal("unavailable identity fallback")
		}
		requireCode(t, b, "service_unavailable")
	}
	if strings.Contains(badLogs.String(), "sentinel") {
		t.Fatal("raw connection error")
	}
}
