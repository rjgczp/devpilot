//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/app"
	"github.com/rjgczp/devpilot/internal/health"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/registration"
	"github.com/rjgczp/devpilot/internal/testdb"
	"github.com/rjgczp/devpilot/internal/user"
	"golang.org/x/crypto/argon2"
)

func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
}

func openDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := testdb.Open(ctx, os.Getenv("TEST_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	version, err := exec.Command("goose", "-env=none", "-version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(version)) != "goose version: v3.26.0" {
		t.Fatal("integration requires pinned native Goose v3.26.0")
	}
	return db
}

func goose(t *testing.T, dir, command string, wantSuccess bool) {
	t.Helper()
	if err := testdb.ValidateURL(os.Getenv("TEST_DATABASE_URL"), os.Getenv("DATABASE_URL")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	args := append([]string{"-env=none", "-dir", dir, "-timeout", "30s"}, strings.Fields(command)...)
	cmd := exec.CommandContext(ctx, "goose", args...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GOOSE_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GOOSE_DRIVER=postgres", "GOOSE_DBSTRING="+os.Getenv("TEST_DATABASE_URL"))
	output, err := cmd.CombinedOutput()
	// Native CLI diagnostics are operator-only and may contain SQL/driver values.
	if (err == nil) != wantSuccess {
		t.Fatalf("native Goose %s success=%v; restricted diagnostic bytes=%d", command, err == nil, len(output))
	}
	t.Logf("native Goose %s expected-success=%v: verified", command, wantSuccess)
}

func migrate(t *testing.T) { goose(t, filepath.Join(root(), "migrations"), "up", true) }

func queryInt(t *testing.T, db *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal("test query failed")
	}
	return n
}

func TestMigrationAndConstraints(t *testing.T) {
	db := openDB(t)
	goose(t, filepath.Join(root(), "migrations"), "down-to 0", true)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	ctx := context.Background()
	if _, err := db.Exec(ctx, `INSERT INTO users(email,password_hash) VALUES('preserved@example.com','future-PHC-with-other-parameters')`); err != nil {
		t.Fatal("parameter-agnostic schema rejected value")
	}
	migrate(t)
	if queryInt(t, db, `SELECT count(*) FROM users WHERE email='preserved@example.com'`) != 1 {
		t.Fatal("repeat up changed data")
	}
	goose(t, filepath.Join(root(), "migrations"), "status", true)
	goose(t, filepath.Join(root(), "migrations"), "down-to 2", true)
	goose(t, filepath.Join(root(), "migrations"), "down", true)
	if queryInt(t, db, `SELECT count(*) FROM users WHERE email='preserved@example.com'`) != 1 || queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name='sessions' AND table_schema='public'`) != 0 {
		t.Fatal("session down damaged users")
	}
	goose(t, filepath.Join(root(), "migrations"), "down", true)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name='users' AND table_schema='public'`) != 0 {
		t.Fatal("down retained table")
	}
	migrate(t)
	for _, tc := range []struct {
		name, sql string
		args      []any
	}{
		{"null-email", `INSERT INTO users(email,password_hash) VALUES(NULL,'hash')`, nil},
		{"null-hash", `INSERT INTO users(email,password_hash) VALUES('a@example.com',NULL)`, nil},
		{"empty-hash", `INSERT INTO users(email,password_hash) VALUES('a@example.com','')`, nil},
		{"long-hash", `INSERT INTO users(email,password_hash) VALUES('a@example.com',$1)`, []any{strings.Repeat("x", 1025)}},
		{"state", `INSERT INTO users(email,password_hash,status) VALUES('a@example.com','hash','admin')`, nil},
		{"uppercase", `INSERT INTO users(email,password_hash) VALUES('A@example.com','hash')`, nil},
		{"space", `INSERT INTO users(email,password_hash) VALUES(' a@example.com','hash')`, nil},
		{"unicode", `INSERT INTO users(email,password_hash) VALUES('国际@example.com','hash')`, nil},
		{"empty-email", `INSERT INTO users(email,password_hash) VALUES('','hash')`, nil},
		{"long-email", `INSERT INTO users(email,password_hash) VALUES($1,'hash')`, []any{strings.Repeat("x", 255)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.Exec(ctx, tc.sql, tc.args...); err == nil {
				t.Fatal("constraint not enforced")
			}
		})
	}
	store := user.Store{Pool: db}
	u, err := store.Create(ctx, "'or'1'='1@example.com", "parameter-independent hash")
	if err != nil || u.Status != "active" || len(u.ID) != 36 || u.ID[14] != '7' || u.CreatedAt.Location() != time.UTC {
		t.Fatal("insert or UUIDv7/defaults incorrect")
	}
	if queryInt(t, db, `SELECT count(*) FROM users WHERE created_at=updated_at AND email_verified_at IS NULL AND id=$1`, u.ID) != 1 {
		t.Fatal("initial account fields")
	}
	if queryInt(t, db, `SELECT count(*) FROM users`) != 1 {
		t.Fatal("parameterized value changed statement behavior")
	}
	if _, err := store.Create(ctx, u.Email, "other hash"); err != user.ErrConflict {
		t.Fatal("unique error classification")
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal("timeout fixture connection")
	}
	if _, err = conn.Exec(ctx, `SET statement_timeout='10ms'`); err != nil {
		t.Fatal("statement timeout fixture")
	}
	_, statementErr := conn.Exec(ctx, `SELECT pg_sleep(0.1)`)
	if user.Classify(ctx, statementErr) != user.ErrTimeout {
		t.Fatal("confirmed PostgreSQL statement timeout classification")
	}
	if _, err = conn.Exec(ctx, `RESET statement_timeout`); err != nil {
		t.Fatal("reset timeout fixture")
	}
	conn.Release()
	dir := t.TempDir()
	original, err := os.ReadFile(filepath.Join(root(), "migrations/00001_create_users.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if os.WriteFile(filepath.Join(dir, "00001_create_users.sql"), original, 0600) != nil {
		t.Fatal("copy migration")
	}
	sessionSQL, err := os.ReadFile(filepath.Join(root(), "migrations/00002_create_sessions.sql"))
	if err != nil || os.WriteFile(filepath.Join(dir, "00002_create_sessions.sql"), sessionSQL, 0600) != nil {
		t.Fatal("copy sessions migration")
	}
	workspaceSQL, err := os.ReadFile(filepath.Join(root(), "migrations/00003_create_workspaces.sql"))
	if err != nil || os.WriteFile(filepath.Join(dir, "00003_create_workspaces.sql"), workspaceSQL, 0600) != nil {
		t.Fatal("copy workspace migration")
	}
	if os.WriteFile(filepath.Join(dir, "00004_intentionally_bad.sql"), []byte("-- +goose Up\nCREATE TABLE must_rollback(id integer);\nSELECT 1/0;\n-- +goose Down\nDROP TABLE must_rollback;\n"), 0600) != nil {
		t.Fatal("write failure fixture")
	}
	goose(t, dir, "up", false)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name='must_rollback'`) != 0 || queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=4 AND is_applied`) != 0 {
		t.Fatal("failed migration partially committed")
	}
}

func newRouter(db *pgxpool.Pool, logs *bytes.Buffer) http.Handler {
	logger := app.NewLogger(logs, slog.LevelInfo)
	var draining atomic.Bool
	return app.NewRoutes(health.Handler{Checker: db, Timeout: time.Second, Draining: &draining, Logger: logger}.Routes(), registration.New(user.Store{Pool: db}, password.New(), logger, password.NewBudget()))
}

const testPassword = "sentinel exact 密码 password 🔐 "

func post(t *testing.T, client *http.Client, url, email, secret string) (int, map[string]json.RawMessage) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"email": email, "password": secret})
	resp, err := client.Post(url+"/api/v1/auth/register", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal("registration HTTP failed")
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "application/json" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Set-Cookie") != "" {
		t.Fatal("HTTP headers")
	}
	var decoded map[string]json.RawMessage
	if json.NewDecoder(resp.Body).Decode(&decoded) != nil {
		t.Fatal("JSON response")
	}
	return resp.StatusCode, decoded
}

func code(t *testing.T, body map[string]json.RawMessage) string {
	t.Helper()
	var e struct{ Code string }
	if json.Unmarshal(body["error"], &e) != nil {
		t.Fatal("missing error")
	}
	return e.Code
}

func TestRegistrationWithPostgreSQL(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	var logs bytes.Buffer
	server := httptest.NewServer(newRouter(db, &logs))
	defer server.Close()
	client := server.Client()
	client.Timeout = 8 * time.Second
	status, body := post(t, client, server.URL, " Alice.Example+tag@Example.COM ", testPassword)
	if status != 201 {
		t.Fatalf("registration status %d", status)
	}
	var u user.Public
	if json.Unmarshal(body["data"], &u) != nil || u.Email != "alice.example+tag@example.com" || u.Status != "active" || len(u.ID) != 36 || u.ID[14] != '7' {
		t.Fatal("public user projection")
	}
	var hash string
	var created, updated time.Time
	var verified *time.Time
	if db.QueryRow(context.Background(), `SELECT password_hash,created_at,updated_at,email_verified_at FROM users WHERE id=$1`, u.ID).Scan(&hash, &created, &updated, &verified) != nil {
		t.Fatal("stored user")
	}
	if !created.Equal(updated) || !u.CreatedAt.Equal(created) || verified != nil {
		t.Fatal("unverified account state")
	}
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[3] != "m=65536,t=3,p=1" {
		t.Fatal("real PHC parameters")
	}
	salt, _ := base64.RawStdEncoding.DecodeString(parts[4])
	key, _ := base64.RawStdEncoding.DecodeString(parts[5])
	if len(salt) != 16 || len(key) != 32 || !bytes.Equal(key, argon2.IDKey([]byte(testPassword), salt, 3, 65536, 1, 32)) {
		t.Fatal("exact password hash")
	}
	if _, err := db.Exec(context.Background(), `UPDATE users SET status='disabled' WHERE id=$1`, u.ID); err != nil {
		t.Fatal("fixture disable")
	}
	status, body = post(t, client, server.URL, "ALICE.EXAMPLE+TAG@example.com", "replacement password phrase")
	if status != 409 || code(t, body) != "email_conflict" {
		t.Fatal("duplicate")
	}
	if queryInt(t, db, `SELECT count(*) FROM users WHERE id=$1 AND status='disabled' AND password_hash=$2 AND created_at=$3 AND updated_at=$4 AND email_verified_at IS NULL`, u.ID, hash, created, updated) != 1 {
		t.Fatal("duplicate changed account")
	}
	// Two overlapping requests fit the two real hash slots and race at UNIQUE.
	statuses := make(chan int, 2)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < 2; i++ {
		go func() {
			start.Wait()
			s, _ := post(t, client, server.URL, "race@example.com", testPassword)
			statuses <- s
		}()
	}
	start.Done()
	a, b := <-statuses, <-statuses
	if !((a == 201 && b == 409) || (a == 409 && b == 201)) {
		t.Fatalf("concurrent results %d/%d", a, b)
	}
	if queryInt(t, db, `SELECT count(*) FROM users WHERE email='race@example.com'`) != 1 {
		t.Fatal("duplicate rows")
	}
	var secondHash string
	if db.QueryRow(context.Background(), `SELECT password_hash FROM users WHERE email='race@example.com'`).Scan(&secondHash) != nil || hash == secondHash {
		t.Fatal("fresh salt")
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := client.Get(server.URL + path)
		if err != nil || resp.StatusCode != 200 {
			t.Fatal("health changed")
		}
		resp.Body.Close()
	}
	t.Run("missing-schema-is500", func(t *testing.T) {
		if _, err := db.Exec(context.Background(), `ALTER TABLE users RENAME TO users_hidden`); err != nil {
			t.Fatal("rename fixture")
		}
		defer func() {
			_, err := db.Exec(context.Background(), `ALTER TABLE users_hidden RENAME TO users`)
			if err != nil {
				t.Fatal("restore fixture")
			}
		}()
		s, body := post(t, client, server.URL, "missing@example.com", testPassword)
		if s != 500 || code(t, body) != "internal_error" {
			t.Fatal("missing schema mapping")
		}
		resp, err := client.Get(server.URL + "/readyz")
		if err != nil || resp.StatusCode != 200 {
			t.Fatal("readiness should only ping")
		}
		resp.Body.Close()
	})
	t.Run("database-deadline", func(t *testing.T) {
		tx, err := db.Begin(context.Background())
		if err != nil {
			t.Fatal("lock transaction")
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(context.Background(), `LOCK TABLE users IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal("lock fixture")
		}
		s, body := post(t, client, server.URL, "timeout@example.com", testPassword)
		if s != 503 || code(t, body) != "database_timeout" {
			t.Fatal("database timeout mapping")
		}
	})
	t.Run("client-cancel-during-insert", func(t *testing.T) {
		tx, err := db.Begin(context.Background())
		if err != nil {
			t.Fatal("transaction")
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(context.Background(), `LOCK TABLE users IN ACCESS EXCLUSIVE MODE`); err != nil {
			t.Fatal("lock")
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		encoded, _ := json.Marshal(map[string]string{"email": "cancel@example.com", "password": testPassword})
		r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/api/v1/auth/register", bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		done := make(chan error, 1)
		go func() {
			resp, err := client.Do(r)
			if resp != nil {
				resp.Body.Close()
			}
			done <- err
		}()
		// Race instrumentation and concurrent hashing can exceed one second before
		// INSERT. Observe within the existing 5s workflow budget, still requiring
		// a real database lock before cancellation (never a sleep-based pass).
		deadline := time.Now().Add(5 * time.Second)
		for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO users%'`) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("INSERT did not reach DB lock")
			}
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
		if <-done == nil {
			t.Fatal("client cancellation unexpectedly succeeded")
		}
		deadline = time.Now().Add(2 * time.Second)
		for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'INSERT INTO users%'`) > 0 {
			if time.Now().After(deadline) {
				t.Fatal("DB cancellation not propagated")
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
	if strings.Contains(logs.String(), "sentinel") || strings.Contains(logs.String(), hash) || strings.Contains(logs.String(), "example.com") || strings.Contains(logs.String(), "SQLSTATE") {
		t.Fatal("sensitive log")
	}
	var secretResponse bytes.Buffer
	_ = json.NewEncoder(&secretResponse).Encode(body)
	if strings.Contains(secretResponse.String(), hash) {
		t.Fatal("hash response")
	}
}

func TestUnavailableDatabaseAndRecovery(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	config, err := pgxpool.ParseConfig("postgres://test:sentinel@127.0.0.1:1/" + testdb.Name + "?sslmode=disable")
	if err != nil {
		t.Fatal("config")
	}
	config.ConnConfig.ConnectTimeout = 100 * time.Millisecond
	bad, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal("pool")
	}
	defer bad.Close()
	var logs bytes.Buffer
	server := httptest.NewServer(newRouter(bad, &logs))
	defer server.Close()
	s, body := post(t, server.Client(), server.URL, "unavailable@example.com", testPassword)
	if s != 503 || code(t, body) != "service_unavailable" {
		t.Fatal("connection mapping")
	}
	resp, err := server.Client().Get(server.URL + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatal("liveness depends on DB")
	}
	resp.Body.Close()
	resp, err = server.Client().Get(server.URL + "/readyz")
	if err != nil || resp.StatusCode != 503 {
		t.Fatal("unavailable readiness")
	}
	resp.Body.Close()
	// Actual same-process pool recovery after the server terminates its connections.
	var recoveryLogs bytes.Buffer
	recovered := httptest.NewServer(newRouter(db, &recoveryLogs))
	defer recovered.Close()
	if _, err = db.Exec(context.Background(), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`); err != nil {
		t.Fatal("disconnect fixture")
	}
	s, _ = post(t, recovered.Client(), recovered.URL, "recovered@example.com", testPassword)
	if s != 201 {
		t.Fatal("pool did not recover")
	}
	if strings.Contains(logs.String(), "sentinel") {
		t.Fatal("connection secret leak")
	}
	t.Log(fmt.Sprintf("verified PostgreSQL18 recovery with native migrations and actual Argon2id"))
}
