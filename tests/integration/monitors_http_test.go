//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func monitorFaultHTTP(t *testing.T, s monitor.Store, token string, p monitor.Public, update bool, status int, code string) {
	t.Helper()
	var logs bytes.Buffer
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := monitor.New(s, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(&logs, nil)))
	method, path := "POST", "/api/v1/workspaces/"+p.WorkspaceID+"/projects/"+p.ProjectID+"/monitors"
	if update {
		method = "PATCH"
		path += "/" + p.ID
	}
	r := httptest.NewRequest(method, path, strings.NewReader(`{"name":"PRIVATE-name","url":"https://example.com/PRIVATE-path"}`))
	r.Header.Set("Cookie", "__Host-devpilot_session="+token)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	r.Header.Set("Content-Type", "application/json")
	if update {
		r.Header.Set("If-Match", monitor.ETag(p))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("status %d expected%d: %s", w.Code, status, w.Body.String())
	}
	requireCode(t, w.Body.Bytes(), code)
	for _, v := range []string{token, p.ID, p.WorkspaceID, "PRIVATE"} {
		if strings.Contains(logs.String()+w.Body.String(), v) {
			t.Fatal("fault leak")
		}
	}
	for _, k := range []string{"Set-Cookie", "ETag", "Location"} {
		if w.Header().Get(k) != "" {
			t.Fatal("fault success header")
		}
	}
}
func TestMonitorHTTPFaultsCancellationAndRecovery(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		for _, tc := range []struct {
			name, outcome string
			fault         error
			status        int
			code          string
		}{
			{name: "lost", outcome: "lost", status: 503, code: "service_unavailable"}, {name: "rollback", outcome: "rollback", status: 503, code: "service_unavailable"},
			{name: "timeout", fault: context.DeadlineExceeded, status: 503, code: "database_timeout"}, {name: "deadlock", fault: &pgconn.PgError{Code: "40P01", Message: "PRIVATE"}, status: 500, code: "internal_error"}, {name: "schema", fault: &pgconn.PgError{Code: "42P01", Message: "PRIVATE"}, status: 500, code: "internal_error"},
		} {
			t.Run(tc.name+map[bool]string{false: "create", true: "update"}[update], func(t *testing.T) {
				f, p := monitorFixture(t, db)
				var commits atomic.Int32
				observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
					if tc.fault != nil {
						return memberObservedTx{Tx: tx, faultStage: "gate", fault: tc.fault}
					}
					return monitorObservedTx{Tx: tx, outcome: tc.outcome, commits: &commits}
				}}
				monitorFaultHTTP(t, monitor.Store{Pool: observed}, f.ownerToken, p, update, tc.status, tc.code)
				if tc.outcome != "" && commits.Load() != 1 {
					t.Fatal("replayed commit")
				}
				if _, err := (monitor.Store{Pool: db}).Detail(context.Background(), f.od, p.WorkspaceID, p.ProjectID, p.ID); err != nil {
					t.Fatal("recovery", err)
				}
			})
		}
	}
	t.Run("actual-schema5", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		if _, err := db.Exec(context.Background(), `ALTER TABLE http_monitors RENAME TO http_monitors_missing_test`); err != nil {
			t.Fatal(err)
		}
		defer db.Exec(context.Background(), `ALTER TABLE http_monitors_missing_test RENAME TO http_monitors`)
		monitorFaultHTTP(t, monitor.Store{Pool: db}, f.ownerToken, p, false, 500, "internal_error")
	})
	t.Run("lock-timeout", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		tx, err := db.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		tx.Exec(context.Background(), `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, p.WorkspaceID)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		awaitMonitor(t, startMonitor(ctx, monitor.Store{Pool: db}, f.od, p, true), user.ErrTimeout)
	})
	t.Run("cancel-after-lock", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
			return monitorObservedTx{Tx: tx, after: func(_ context.Context, stage string) {
				if stage == "session" {
					cancel()
				}
			}}
		}}}
		awaitMonitor(t, startMonitor(ctx, s, f.od, p, true), user.ErrCanceled)
	})
	t.Run("real-pool-timeout", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		cfg := db.Config()
		cfg.MaxConns = 1
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		conn, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Release()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		awaitMonitor(t, startMonitor(ctx, monitor.Store{Pool: pool}, f.od, p, false), user.ErrTimeout)
	})
	t.Run("actual-query-timeout", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		tx, err := db.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		tx.Exec(context.Background(), `LOCK TABLE http_monitors IN ACCESS EXCLUSIVE MODE`)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		_, err = (monitor.Store{Pool: db}).Detail(ctx, f.od, p.WorkspaceID, p.ProjectID, p.ID)
		monitorWant(t, err, user.ErrTimeout)
	})
	if queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`) != 0 {
		t.Fatal("leaked transaction")
	}
}

func monitorHTTP(t *testing.T, c *http.Client, base, method, path, body string, tag ...string) (int, http.Header, []byte) {
	t.Helper()
	if method == "GET" {
		tag = nil
	}
	return workspaceRequest(t, c, base, method, path, body, true, tag...)
}
func TestMonitorHTTPJourneyAndIsolation(t *testing.T) {
	db := workspaceDB(t)
	var logs bytes.Buffer
	server, owner := authServer(t, db, false, &logs)
	for _, email := range []string{"monitor-owner@example.com", "monitor-member@example.com"} {
		status, _ := post(t, owner, server.URL, email, testPassword)
		if status != 201 {
			t.Fatal("register")
		}
	}
	status, _, _ := authRequest(t, owner, server.URL, "POST", "login", loginBody("monitor-owner@example.com", testPassword), server.URL)
	if status != 200 {
		t.Fatal("login")
	}
	w := workspaceHTTPCreate(t, owner, server.URL, "Project tenant")
	member := workspaceCookieClient(t, server, "unused", false)
	status, _, _ = authRequest(t, member, server.URL, "POST", "login", loginBody("monitor-member@example.com", testPassword), server.URL)
	if status != 200 {
		t.Fatal("member login")
	}
	status, _, body := authRequest(t, member, server.URL, "GET", "me", "", "")
	var me struct{ Data user.Public }
	if status != 200 || json.Unmarshal(body, &me) != nil {
		t.Fatal("me")
	}
	var parent struct{ Data project.Public }
	status, _, body = projectHTTP(t, owner, server.URL, "POST", "/api/v1/workspaces/"+w.ID+"/projects", `{"name":"parent"}`)
	if status != 201 || json.Unmarshal(body, &parent) != nil {
		t.Fatal("parent")
	}
	path := "/api/v1/workspaces/" + w.ID + "/projects/" + parent.Data.ID + "/monitors"
	status, _, body = monitorHTTP(t, owner, server.URL, "POST", "/api/v1/workspaces/"+w.ID+"/invitations", `{"invitee_user_id":"`+me.Data.ID+`"}`)
	var inv struct{ Data workspace.OwnerInvitation }
	if status != 201 || json.Unmarshal(body, &inv) != nil {
		t.Fatal("invitation", status, string(body))
	}
	status, _, _ = monitorHTTP(t, member, server.URL, "GET", path, "")
	if status != 404 {
		t.Fatal("pending scope")
	}
	status, _, body = monitorHTTP(t, member, server.URL, "POST", "/api/v1/invitations/"+inv.Data.ID+"/accept", "")
	var acceptance struct{ Data workspace.Acceptance }
	if status != 200 || json.Unmarshal(body, &acceptance) != nil {
		t.Fatal("accept")
	}
	status, h, body := monitorHTTP(t, owner, server.URL, "POST", path, `{"name":"配置","url":"https://example.com/private-path"}`)
	var p struct{ Data monitor.Public }
	if status != 201 || json.Unmarshal(body, &p) != nil || h.Get("ETag") != monitor.ETag(p.Data) || h.Get("Location") != path+"/"+p.Data.ID {
		t.Fatal("create", status, string(body))
	}
	item := path + "/" + p.Data.ID
	tag := h.Get("ETag")

	// Activation is a separate offline resource; configuration tag stays v1.
	status, ah, ab := monitorHTTP(t, member, server.URL, "GET", item+"/activation", "")
	var activation struct{ Data monitor.Activation }
	if status != 200 || json.Unmarshal(ab, &activation) != nil || activation.Data.ActivationGeneration != "1" {
		t.Fatal("activation default", status, string(ab))
	}
	command := `{"desired_state":"enabled","configuration_version":"1"}`
	status, _, _ = monitorHTTP(t, member, server.URL, "PUT", item+"/activation", command, ah.Get("ETag"))
	if status != 403 {
		t.Fatal("Member activation write")
	}
	status, ah, ab = monitorHTTP(t, owner, server.URL, "PUT", item+"/activation", command, ah.Get("ETag"))
	if status != 200 || json.Unmarshal(ab, &activation) != nil || activation.Data.DesiredState != "enabled" || activation.Data.ExecutionState != "blocked" || activation.Data.NextDueAt != nil {
		t.Fatal("activation enable", status, string(ab))
	}
	status, _, _ = monitorHTTP(t, owner, server.URL, "PUT", item+"/activation", command, ah.Get("ETag"))
	if status != 429 {
		t.Fatal("activation cooling")
	}
	status, _, ab = monitorHTTP(t, owner, server.URL, "PUT", item+"/activation", `{"desired_state":"disabled","configuration_version":"1"}`, ah.Get("ETag"))
	if status != 200 {
		t.Fatal("activation pause", string(ab))
	}
	// Restart the actual API listener with the same database; cookies/session and
	// durable intent survive, and no target operation is scheduled on startup.
	server.Close()
	server, _ = authServer(t, db, false, &logs)
	status, _, ab = monitorHTTP(t, owner, server.URL, "GET", item+"/activation", "")
	if status != 200 || json.Unmarshal(ab, &activation) != nil || activation.Data.DesiredState != "disabled" || activation.Data.ActivationGeneration != "3" {
		t.Fatal("restart state", status, string(ab))
	}
	for _, route := range []string{path, item} {
		status, _, _ = monitorHTTP(t, member, server.URL, "GET", route, "")
		if status != 200 {
			t.Fatal("member read")
		}
	}
	status, _, body = monitorHTTP(t, member, server.URL, "PATCH", item, `{"name":"no"}`, tag)
	if status != 403 {
		t.Fatal("member write")
	}
	requireCode(t, body, "monitor_forbidden")
	status, _, _ = monitorHTTP(t, owner, server.URL, "PATCH", item, `{"name":"no"}`)
	if status != 428 {
		t.Fatal("missing tag")
	}
	status, h, body = monitorHTTP(t, owner, server.URL, "PATCH", item, `{"method":"GET"}`, tag)
	if status != 200 || json.Unmarshal(body, &p) != nil || p.Data.Version != "2" || p.Data.Method != "GET" {
		t.Fatal("patch", status, string(body))
	}
	status, _, ab = monitorHTTP(t, owner, server.URL, "GET", item+"/activation", "")
	if status != 200 || json.Unmarshal(ab, &activation) != nil || activation.Data.LastTransitionKind != "configuration_changed" || activation.Data.ActivationGeneration != "4" || activation.Data.ConfigurationVersion != "2" {
		t.Fatal("patch revocation")
	}
	status, _, _ = monitorHTTP(t, owner, server.URL, "PATCH", item, `{"name":"no"}`, tag)
	if status != 412 {
		t.Fatal("replay")
	}
	z := workspaceHTTPCreate(t, member, server.URL, "other tenant")
	for _, route := range []string{"/api/v1/workspaces/" + z.ID + "/projects/" + parent.Data.ID + "/monitors/" + p.Data.ID, path + "/" + z.ID, "/api/v1/workspaces/bad/projects/" + parent.Data.ID + "/monitors/" + p.Data.ID} {
		for _, m := range []string{"GET", "PATCH"} {
			status, headers, b := monitorHTTP(t, member, server.URL, m, route, `{"name":"no"}`, tag)
			if status != 404 || headers.Get("ETag") != "" || headers.Get("Location") != "" {
				t.Fatal("IDOR", status)
			}
			requireCode(t, b, "monitor_not_found")
		}
	}
	status, _, _ = monitorHTTP(t, member, server.URL, "DELETE", "/api/v1/workspaces/"+w.ID+"/members/"+me.Data.ID, "", workspace.MemberCondition(acceptance.Data.MembershipID, "member"))
	if status != 204 {
		t.Fatal("exit")
	}
	status, _, _ = monitorHTTP(t, member, server.URL, "GET", item, "")
	if status != 404 {
		t.Fatal("fresh authority")
	}
	status, _, _ = authRequest(t, owner, server.URL, "POST", "logout", "", server.URL)
	if status != 204 {
		t.Fatal("logout")
	}
	status, _, _ = monitorHTTP(t, owner, server.URL, "GET", item, "")
	if status != 401 {
		t.Fatal("fresh revocation")
	}
	if strings.Contains(logs.String(), p.Data.ID) || strings.Contains(logs.String(), "line") {
		t.Fatal("private project logging")
	}
}
