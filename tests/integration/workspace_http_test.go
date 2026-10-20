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
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/auth"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func workspaceRequest(t *testing.T, client *http.Client, base, method, path, body string, proof bool, conditions ...string) (int, http.Header, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal("workspace HTTP fixture")
	}
	if method == "POST" || method == "DELETE" || method == "PATCH" || method == "PUT" {
		if method == "POST" || method == "PATCH" || method == "PUT" {
			r.Header.Set("Content-Type", "application/json")
		}
		if proof {
			r.Header.Set("Origin", base)
			r.Header.Set("X-DevPilot-CSRF", "1")
		}
	}
	if len(conditions) > 0 {
		r.Header.Set("If-Match", conditions[0])
	}
	resp, err := client.Do(r)
	if err != nil {
		t.Fatal("workspace HTTP failed")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal("workspace HTTP body")
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Set-Cookie") != "" || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("workspace HTTP headers")
	}
	return resp.StatusCode, resp.Header, b
}
func workspaceHTTPCreate(t *testing.T, client *http.Client, base, name string) workspace.Public {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"name": name})
	status, h, body := workspaceRequest(t, client, base, "POST", "/api/v1/workspaces", string(b), true)
	var out struct{ Data workspace.Public }
	if status != 201 || json.Unmarshal(body, &out) != nil || h.Get("Location") != "/api/v1/workspaces/"+out.Data.ID || out.Data.Role != "owner" {
		t.Fatal("workspace HTTP creation")
	}
	return out.Data
}
func workspaceCookieClient(t *testing.T, server *httptest.Server, token string, tls bool) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal("jar")
	}
	base, _ := url.Parse(server.URL)
	mode := "development"
	if tls {
		mode = "production"
	}
	jar.SetCookies(base, []*http.Cookie{{Name: auth.CookieName(config.AuthConfig{Mode: mode}), Value: token, Path: "/", Secure: tls}})
	return &http.Client{Transport: server.Client().Transport, Jar: jar, Timeout: 8 * time.Second}
}

func TestWorkspaceHTTPAuthenticationAndTenantIsolation(t *testing.T) {
	db := workspaceDB(t)
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "development", true: "production"}[tls], func(t *testing.T) {
			ctx := context.Background()
			var logs bytes.Buffer
			server, a := authServer(t, db, tls, &logs)
			suffix := "http"
			if tls {
				suffix = "tls"
			}
			email := "workspace-e2e-" + suffix + "@example.com"
			if status, _ := post(t, a, server.URL, email, testPassword); status != 201 {
				t.Fatal("register")
			}
			if status, _, _ := authRequest(t, a, server.URL, "POST", "login", loginBody(email, testPassword), server.URL); status != 200 {
				t.Fatal("login")
			}
			x := workspaceHTTPCreate(t, a, server.URL, "SENSITIVE_WORKSPACE_NAME")
			second := workspaceHTTPCreate(t, a, server.URL, "SENSITIVE_WORKSPACE_NAME")
			if second.ID == x.ID {
				t.Fatal("same-name creation deduplicated")
			}
			ub, bSecret, bDigest := workspaceIdentity(t, db, "e2e-b-"+suffix+"@example.com")
			uc, cSecret, cDigest := workspaceIdentity(t, db, "e2e-c-"+suffix+"@example.com")
			b := workspaceCookieClient(t, server, bSecret, tls)
			c := workspaceCookieClient(t, server, cSecret, tls)
			y := workspaceHTTPCreate(t, b, server.URL, "Y")
			z := workspaceHTTPCreate(t, b, server.URL, "Z")
			if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) SELECT $1,id,'member' FROM users WHERE email=$2`, y.ID, email); err != nil {
				t.Fatal("A member Y")
			}
			if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, x.ID, ub.ID); err != nil {
				t.Fatal("B member X")
			}
			status, _, body := workspaceRequest(t, c, server.URL, "GET", "/api/v1/workspaces", "", false)
			if status != 200 || string(body) != "{\"data\":[],\"pagination\":{\"next_after\":null}}\n" {
				t.Fatal("empty membership list")
			}
			var hidden []byte
			for _, id := range []string{z.ID, "00000000-0000-0000-0000-000000000000", "not-a-uuid", strings.ToUpper(x.ID)} {
				status, _, body := workspaceRequest(t, a, server.URL, "GET", "/api/v1/workspaces/"+id, "", false)
				if status != 404 {
					t.Fatal("cross-tenant UUID or malformed path not hidden")
				}
				requireCode(t, body, "workspace_not_found")
				if hidden == nil {
					hidden = body
				} else if !bytes.Equal(hidden, body) {
					t.Fatal("existence disclosure")
				}
			}
			status, _, body = workspaceRequest(t, c, server.URL, "GET", "/api/v1/workspaces/"+x.ID, "", false)
			if status != 404 || !bytes.Equal(hidden, body) {
				t.Fatal("outsider detail")
			}
			status, _, body = workspaceRequest(t, a, server.URL, "GET", "/api/v1/workspaces/"+y.ID, "", false)
			var detail struct{ Data workspace.Public }
			if status != 200 || json.Unmarshal(body, &detail) != nil || detail.Data.Role != "member" {
				t.Fatal("member access")
			}
			for _, cursor := range []string{z.ID, "00000000-0000-0000-0000-000000000000"} {
				status, _, body = workspaceRequest(t, a, server.URL, "GET", "/api/v1/workspaces?limit=1&after="+cursor, "", false)
				var page workspace.Page
				if status != 200 || json.Unmarshal(body, &page) != nil || len(page.Data) > 1 {
					t.Fatal("cursor contract")
				}
				for _, row := range page.Data {
					if row.ID == z.ID {
						t.Fatal("cursor gained tenant access")
					}
				}
			}
			before := queryInt(t, db, `SELECT count(*) FROM workspaces`)
			for _, body := range []string{`{"name":"x","user_id":"` + uc.ID + `"}`, `{"name":"x","role":"owner"}`} {
				status, _, out := workspaceRequest(t, a, server.URL, "POST", "/api/v1/workspaces", body, true)
				if status != 400 {
					t.Fatal("forged owner accepted")
				}
				requireCode(t, out, "invalid_request")
			}
			status, _, body = workspaceRequest(t, a, server.URL, "POST", "/api/v1/workspaces", `{"name":"x"}`, false)
			if status != 403 {
				t.Fatal("CSRF")
			}
			if queryInt(t, db, `SELECT count(*) FROM workspaces`) != before {
				t.Fatal("rejected HTTP request wrote")
			}
			status, _, _ = workspaceRequest(t, a, server.URL, "GET", "/api/v1/workspaces?user_id="+ub.ID, "", false)
			if status != 400 {
				t.Fatal("forged query identity accepted")
			}
			for _, path := range []string{"/api/v1/workspaces", "/api/v1/workspaces/" + x.ID} {
				for _, method := range []string{"HEAD", "OPTIONS", "PATCH", "DELETE"} {
					status, h, b := workspaceRequest(t, a, server.URL, method, path, "", false)
					allow := "GET"
					if path == "/api/v1/workspaces" {
						allow = "GET, POST"
					}
					if status != 405 || h.Get("Allow") != allow || (method == "HEAD" && len(b) != 0) {
						t.Fatal("real router method contract")
					}
				}
			}
			extra, err := a.Get(server.URL + "/api/v1/workspaces/" + x.ID + "/members")
			if err != nil {
				t.Fatal("extra route request")
			}
			extra.Body.Close()
			if extra.StatusCode != 200 {
				t.Fatal("member roster route or redirect")
			}
			// All live-state failures share 401, no deletion Cookie.
			if _, err := db.Exec(ctx, `UPDATE sessions SET created_at=statement_timestamp()-interval '2h',expires_at=statement_timestamp()-interval '1h' WHERE token_hash=$1`, cDigest[:]); err != nil {
				t.Fatal("expiry fixture")
			}
			if (session.Store{Pool: db}).Revoke(ctx, bDigest) != nil {
				t.Fatal("revoke fixture")
			}
			if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE email=$1`, email); err != nil {
				t.Fatal("disable fixture")
			}
			unknown, _, err := session.NewTokens().New()
			if err != nil {
				t.Fatal("unknown token fixture")
			}
			for _, client := range []*http.Client{a, b, c, workspaceCookieClient(t, server, unknown, tls), workspaceCookieClient(t, server, "malformed", tls), workspaceCookieClient(t, server, "", tls)} {
				status, _, body := workspaceRequest(t, client, server.URL, "GET", "/api/v1/workspaces", "", false)
				if status != 401 {
					t.Fatal("invalid live identity was not 401")
				}
				requireCode(t, body, "unauthenticated")
			}
			if strings.Contains(logs.String(), testPassword) || strings.Contains(logs.String(), bSecret) || strings.Contains(logs.String(), "SENSITIVE_WORKSPACE_NAME") {
				t.Fatal("sensitive HTTP diagnostic")
			}
		})
	}
}

func TestWorkspaceHTTPDatabaseFailuresAndRecovery(t *testing.T) {
	db := workspaceDB(t)
	u, secret, digest := workspaceIdentity(t, db, "workspace-db-errors@example.com")
	origin, _ := config.ParseOrigin("http://127.0.0.1:8080")
	policy := config.AuthConfig{Mode: "development", Origin: origin}
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	perform := func(store workspace.Database, method string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://127.0.0.1:8080/api/v1/workspaces", strings.NewReader(`{"name":"SENSITIVE_NAME"}`))
		r.Header.Set("Cookie", "devpilot_session="+secret)
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("X-DevPilot-CSRF", "1")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		workspace.New(workspace.Store{Pool: store}, policy, logger).ServeHTTP(w, r)
		if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" && w.Code != 201 {
			t.Fatal("fault response leaked success artifact")
		}
		return w
	}
	for _, fault := range []*pgconn.PgError{{Code: "57014", Message: "SENSITIVE unexplained cancellation"}, {Code: "23505", ConstraintName: "users_email_key", Message: "SENSITIVE email"}} {
		w := perform(workspaceObservedDB{Pool: db, beginErr: fault}, "POST")
		if w.Code != 500 {
			t.Fatal("unsafe fault classification")
		}
		requireCode(t, w.Body.Bytes(), "internal_error")
	}
	// Both unknown commit outcomes must remain safe HTTP errors, without retry.
	for _, outcome := range []string{"lost", "rollback"} {
		before := queryInt(t, db, `SELECT count(*) FROM workspaces`)
		observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome} }}
		w := perform(observed, "POST")
		if w.Code != 503 {
			t.Fatal("unknown commit success")
		}
		requireCode(t, w.Body.Bytes(), "service_unavailable")
		want := before
		if outcome == "lost" {
			want++
		}
		if queryInt(t, db, `SELECT count(*) FROM workspaces`) != want {
			t.Fatal("HTTP unknown commit storage")
		}
	}
	// A real row lock held by another transaction exceeds the handler's 2s DB
	// budget, then recovery succeeds after rollback.
	tx, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal("lock fixture")
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(context.Background(), `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
		t.Fatal("lock fixture")
	}
	w := perform(db, "POST")
	if w.Code != 503 {
		t.Fatal("lock timeout HTTP")
	}
	requireCode(t, w.Body.Bytes(), "database_timeout")
	if tx.Rollback(context.Background()) != nil {
		t.Fatal("lock release")
	}
	if w = perform(db, "POST"); w.Code != 201 {
		t.Fatal("recovery after lock timeout")
	}
	if _, err = db.Exec(context.Background(), `ALTER TABLE workspaces RENAME TO workspace_missing_fixture`); err != nil {
		t.Fatal("missing schema fixture")
	}
	w = perform(db, "GET")
	if _, err = db.Exec(context.Background(), `ALTER TABLE workspace_missing_fixture RENAME TO workspaces`); err != nil {
		t.Fatal("schema restore")
	}
	if w.Code != 500 {
		t.Fatal("missing schema not internal")
	}
	requireCode(t, w.Body.Bytes(), "internal_error")
	badConfig, err := pgxpool.ParseConfig("postgres://test:SENSITIVE_DSN@127.0.0.1:1/devpilot_registration_test?sslmode=disable")
	if err != nil {
		t.Fatal("bad pool fixture")
	}
	badConfig.ConnConfig.ConnectTimeout = 100 * time.Millisecond
	bad, err := pgxpool.NewWithConfig(context.Background(), badConfig)
	if err != nil {
		t.Fatal("bad pool")
	}
	defer bad.Close()
	w = perform(bad, "GET")
	if w.Code != 503 {
		t.Fatal("unavailable DB")
	}
	requireCode(t, w.Body.Bytes(), "service_unavailable")
	if w = perform(db, "GET"); w.Code != 200 {
		t.Fatal("same-process DB recovery")
	}
	if strings.Contains(logs.String(), "SENSITIVE") || strings.Contains(logs.String(), secret) {
		t.Fatal("raw error/secret/name logged")
	}
	if _, err := (workspace.Store{Pool: db}).Detail(context.Background(), digest, "bad"); err != workspace.ErrNotFound {
		t.Fatal("malformed selector after live authentication")
	}
}
