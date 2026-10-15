//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/config"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func memberFaultResponse(t *testing.T, store workspace.Store, token, wid, target string, condition string, status int, code string) {
	t.Helper()
	var logs bytes.Buffer
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := workspace.New(store, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(&logs, nil)))
	r := httptest.NewRequest("DELETE", "/api/v1/workspaces/"+wid+"/members/"+target, nil)
	r.Header.Set("Cookie", "__Host-devpilot_session="+token)
	r.Header.Set("If-Match", condition)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("member failure status %d want %d", w.Code, status)
	}
	requireCode(t, w.Body.Bytes(), code)
	if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" || strings.Contains(logs.String()+w.Body.String(), "SENSITIVE") || strings.Contains(logs.String(), wid) || strings.Contains(logs.String(), token) {
		t.Fatal("member fault privacy")
	}
}
func TestMemberTransactionFailures(t *testing.T) {
	db := workspaceDB(t)
	for _, tc := range []struct {
		name, outcome string
		fault         error
		status        int
		code          string
	}{
		{name: "lost", outcome: "lost", status: 503, code: "service_unavailable"},
		{name: "rollback", outcome: "rollback", status: 503, code: "service_unavailable"},
		{name: "gate-unavailable", fault: io.ErrUnexpectedEOF, status: 503, code: "service_unavailable"},
		{name: "gate-deadline", fault: context.DeadlineExceeded, status: 503, code: "database_timeout"},
		{name: "schema", fault: &pgconn.PgError{Code: "42P01", Message: "SENSITIVE"}, status: 500, code: "internal_error"},
		{name: "deadlock", fault: &pgconn.PgError{Code: "40P01", Message: "SENSITIVE"}, status: 500, code: "internal_error"},
		{name: "serialization", fault: &pgconn.PgError{Code: "40001", Message: "SENSITIVE"}, status: 500, code: "internal_error"},
		{name: "unexplained-cancel", fault: &pgconn.PgError{Code: "57014", Message: "SENSITIVE"}, status: 500, code: "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := membersFixture(t, db)
			var commits atomic.Int32
			observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				stage := ""
				if tc.fault != nil {
					stage = "gate"
				}
				return memberObservedTx{Tx: tx, commitOutcome: tc.outcome, commits: &commits, faultStage: stage, fault: tc.fault}
			}}
			memberFaultResponse(t, workspace.Store{Pool: observed}, f.ownerToken, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID), tc.status, tc.code)
			want := 1
			if tc.outcome == "lost" {
				want = 0
			}
			if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != want {
				t.Fatal("uncertain/rollback actual state")
			}
			if tc.outcome != "" && commits.Load() != 1 || tc.fault != nil && commits.Load() != 0 {
				t.Fatal("automatic commit replay")
			}
			// A fresh authorized request recovers without process-global failure state.
			if err := (workspace.Store{Pool: db}).DeleteMember(context.Background(), f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil && !(tc.outcome == "lost" && errors.Is(err, workspace.ErrPreconditionFailed)) {
				t.Fatal("member fault recovery", err)
			}
		})
	}
	t.Run("actual-lock-timeout", func(t *testing.T) {
		f := membersFixture(t, db)
		ctx := context.Background()
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal("blocker")
		}
		defer tx.Rollback(ctx)
		if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID); err != nil {
			t.Fatal("workspace blocker")
		}
		pid := make(chan int, 1)
		done := make(chan struct{})
		go func() {
			defer close(done)
			memberFaultResponse(t, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.ownerToken, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID), 503, "database_timeout")
		}()
		waitWorkspaceBlock(t, db, <-pid)
		select {
		case <-done:
		case <-time.After(4 * time.Second):
			t.Fatal("unbounded member lock budget")
		}
		if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
			t.Fatal("timed out mutation persisted")
		}
	})
	t.Run("cancel-after-delete-gate", func(t *testing.T) {
		f := membersFixture(t, db)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
			return memberObservedTx{Tx: tx, after: func(_ context.Context, stage string) {
				if stage == "gate" {
					cancel()
				}
			}}
		}}
		err := (workspace.Store{Pool: observed}).DeleteMember(ctx, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID))
		if err == nil || !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
			t.Fatal("cancellation not propagated", err)
		}
		if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
			t.Fatal("canceled delete committed")
		}
	})
}

func TestMemberPoolTimeoutAndMissingSchema(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("pool fixture config")
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("pool fixture")
	}
	defer pool.Close()
	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("pool acquire")
	}
	memberFaultResponse(t, workspace.Store{Pool: pool}, f.ownerToken, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID), 503, "database_timeout")
	held.Release()
	if _, err := db.Exec(ctx, `ALTER TABLE workspace_members RENAME TO member_fixture_hidden`); err != nil {
		t.Fatal("guarded schema fixture")
	}
	defer func() {
		if _, err := db.Exec(ctx, `ALTER TABLE member_fixture_hidden RENAME TO workspace_members`); err != nil {
			t.Error("schema fixture restore")
		}
	}()
	memberFaultResponse(t, workspace.Store{Pool: db}, f.ownerToken, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID), 500, "internal_error")
	if _, err := (workspace.Store{Pool: db}).ListMembers(ctx, f.od, f.w.ID, 50, ""); err == nil {
		t.Fatal("roster accepted missing schema")
	}
}

func TestMemberHTTPCancellationAfterGate(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return memberObservedTx{Tx: tx, after: func(_ context.Context, stage string) {
			if stage == "gate" {
				cancel()
			}
		}}
	}}
	origin, _ := config.ParseOrigin("https://app.example.com")
	h := workspace.New(workspace.Store{Pool: observed}, config.AuthConfig{Mode: "production", Origin: origin}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	r := httptest.NewRequest("DELETE", "/api/v1/workspaces/"+f.w.ID+"/members/"+f.member.ID, nil).WithContext(ctx)
	r.Header.Set("If-Match", f.condition(f.w.ID, f.member.ID))
	r.Header.Set("Cookie", "__Host-devpilot_session="+f.ownerToken)
	r.Header.Set("Origin", "https://app.example.com")
	r.Header.Set("X-DevPilot-CSRF", "1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatal("canceled request attempted response")
	}
	if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
		t.Fatal("canceled HTTP delete committed")
	}
}
