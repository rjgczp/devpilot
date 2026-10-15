//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func invitationFaultHTTP(t *testing.T, s workspace.Store, token, method, path, body string, status int, code string) {
	t.Helper()
	origin, _ := config.ParseOrigin("https://app.example.com")
	var logs bytes.Buffer
	h := workspace.New(s, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(&logs, nil)))
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cookie", "__Host-devpilot_session="+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("invitation fault status %d want %d: %s", w.Code, status, w.Body.String())
	}
	requireCode(t, w.Body.Bytes(), code)
	if w.Header().Get("Set-Cookie") != "" || strings.Contains(w.Body.String()+logs.String(), token) || strings.Contains(logs.String(), "SENSITIVE") {
		t.Fatal("unsafe invitation failure")
	}
}
func TestInvitationHTTPFaultsAndCancellation(t *testing.T) {
	db := workspaceDB(t)
	for _, action := range []string{"create", "accept", "decline", "revoke"} {
		for _, fault := range []string{"begin", "gate", "lost", "rollback", "cancel"} {
			t.Run(action+"-"+fault, func(t *testing.T) {
				f := invitationFixture(t, db)
				var i workspace.OwnerInvitation
				if action != "create" {
					i = createInvitation(t, db, f)
				}
				token, method, path, body := f.memberToken, "POST", "/api/v1/invitations/"+i.ID+"/"+action, ""
				if action == "create" {
					token = f.ownerToken
					path = "/api/v1/workspaces/" + f.w.ID + "/invitations"
					body = `{"invitee_user_id":"` + f.member.ID + `"}`
				}
				if action == "revoke" {
					token = f.ownerToken
					method = "DELETE"
					path = "/api/v1/invitations/" + i.ID
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				observed := workspaceObservedDB{Pool: db}
				if fault == "begin" {
					observed.beginErr = io.ErrUnexpectedEOF
				} else {
					observed.wrap = func(tx pgx.Tx) pgx.Tx {
						v := invitationObservedTx{Tx: tx}
						if fault == "gate" {
							v.faultStage = "gate"
						}
						if fault == "lost" || fault == "rollback" {
							v.outcome = fault
						}
						if fault == "cancel" {
							v.after = func(_ context.Context, stage string) {
								if stage == "gate" {
									cancel()
								}
							}
						}
						return v
					}
				}
				s := workspace.Store{Pool: observed}
				if fault != "cancel" {
					invitationFaultHTTP(t, s, token, method, path, body, 503, "service_unavailable")
				} else {
					origin, _ := config.ParseOrigin("https://app.example.com")
					var logs bytes.Buffer
					h := workspace.New(s, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(&logs, nil)))
					r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
					r.Header.Set("Origin", "https://app.example.com")
					r.Header.Set("X-DevPilot-CSRF", "1")
					r.Header.Set("Content-Type", "application/json")
					r.Header.Set("Cookie", "__Host-devpilot_session="+token)
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "" {
						t.Fatal("response after cancel")
					}
				}
			})
		}
	}
	t.Run("actual-lock-timeout", func(t *testing.T) {
		f := invitationFixture(t, db)
		i := createInvitation(t, db, f)
		tx, err := db.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(context.Background(), `SELECT id FROM workspace_invitations WHERE id=$1 FOR UPDATE`, i.ID); err != nil {
			t.Fatal(err)
		}
		invitationFaultHTTP(t, workspace.Store{Pool: db}, f.memberToken, "POST", "/api/v1/invitations/"+i.ID+"/accept", "", 503, "database_timeout")
	})
	t.Run("actual-pool-timeout", func(t *testing.T) {
		f := invitationFixture(t, db)
		i := createInvitation(t, db, f)
		cfg, err := pgxpool.ParseConfig(testURL())
		if err != nil {
			t.Fatal(err)
		}
		cfg.MaxConns = 1
		pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		held, err := pool.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer held.Release()
		invitationFaultHTTP(t, workspace.Store{Pool: pool}, f.memberToken, "POST", "/api/v1/invitations/"+i.ID+"/accept", "", 503, "database_timeout")
	})
	t.Run("missing-schema", func(t *testing.T) {
		f := invitationFixture(t, db)
		i := createInvitation(t, db, f)
		if _, err := db.Exec(context.Background(), `ALTER TABLE workspace_invitations RENAME TO invitation_fixture_hidden`); err != nil {
			t.Fatal(err)
		}
		defer db.Exec(context.Background(), `ALTER TABLE invitation_fixture_hidden RENAME TO workspace_invitations`)
		invitationFaultHTTP(t, workspace.Store{Pool: db}, f.memberToken, "POST", "/api/v1/invitations/"+i.ID+"/accept", "", 500, "internal_error")
	})
}

// Connection configuration is still validated by workspaceDB before use.
func testURL() string { return os.Getenv("TEST_DATABASE_URL") }
