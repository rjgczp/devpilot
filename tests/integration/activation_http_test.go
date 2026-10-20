//go:build integration

package integration

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/monitor"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivationHTTPUnknownCommitAndFaults(t *testing.T) {
	db := workspaceDB(t)
	for _, tc := range []struct {
		name, outcome string
		fault         error
		status        int
		code          string
	}{{name: "lost", outcome: "lost", status: 503, code: "service_unavailable"}, {name: "rollback", outcome: "rollback", status: 503, code: "service_unavailable"}, {name: "deadlock", fault: &pgconn.PgError{Code: "40P01", Message: "PRIVATE"}, status: 500, code: "internal_error"}, {name: "schema", fault: &pgconn.PgError{Code: "42P01", Message: "PRIVATE"}, status: 500, code: "internal_error"}, {name: "timeout", fault: context.DeadlineExceeded, status: 503, code: "database_timeout"}} {
		t.Run(tc.name, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			a := activationRead(t, db, f.od, m)
			observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				if tc.fault != nil {
					return memberObservedTx{Tx: tx, faultStage: "gate", fault: tc.fault}
				}
				return activationObservedTx{Tx: tx, outcome: tc.outcome}
			}}
			var logs bytes.Buffer
			origin, _ := config.ParseOrigin("https://app.example.com")
			h := monitor.New(monitor.Store{Pool: observed}, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(&logs, nil)))
			r := httptest.NewRequest("PUT", "/api/v1/workspaces/"+m.WorkspaceID+"/projects/"+m.ProjectID+"/monitors/"+m.ID+"/activation", strings.NewReader(`{"desired_state":"enabled","configuration_version":"1"}`))
			r.Header.Set("Cookie", "__Host-devpilot_session="+f.ownerToken)
			r.Header.Set("Origin", "https://app.example.com")
			r.Header.Set("X-DevPilot-CSRF", "1")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("If-Match", monitor.ActivationETag(a))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatal("fault status", w.Code)
			}
			requireCode(t, w.Body.Bytes(), tc.code)
			for _, v := range []string{f.ownerToken, m.ID, "PRIVATE", "private-path"} {
				if strings.Contains(logs.String()+w.Body.String(), v) {
					t.Fatal("sensitive fault output")
				}
			}
			if w.Header().Get("ETag") != "" || w.Header().Get("Location") != "" || w.Header().Get("Set-Cookie") != "" {
				t.Fatal("unconfirmed success metadata")
			}
			current := activationRead(t, db, f.od, m)
			want := "1"
			if tc.outcome == "lost" {
				want = "2"
			}
			if current.ActivationGeneration != want {
				t.Fatal("commit reconciliation")
			}
		})
	}
	t.Run("actual-schema6", func(t *testing.T) {
		f, m := monitorFixture(t, db)
		if _, e := db.Exec(context.Background(), `ALTER TABLE monitor_activation_states RENAME TO activation_missing_test`); e != nil {
			t.Fatal(e)
		}
		defer db.Exec(context.Background(), `ALTER TABLE activation_missing_test RENAME TO monitor_activation_states`)
		_, e := (monitor.Store{Pool: db}).ActivationDetail(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID)
		if e == nil {
			t.Fatal("missing schema unexpectedly succeeded")
		}
	})
}
