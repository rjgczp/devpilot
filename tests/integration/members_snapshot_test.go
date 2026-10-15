//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func TestMemberRosterSnapshotAndCursorRevocation(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"remove", "role", "revoke", "disable"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			f := membersFixture(t, db)
			p := newWorkspacePause(t)
			observed := workspaceObservedDB{Pool: db, read: p.stop}
			type result struct {
				page workspace.MemberPage
				err  error
			}
			done := make(chan result, 1)
			go func() {
				page, err := (workspace.Store{Pool: observed}).ListMembers(ctx, f.md, f.w.ID, 1, "")
				done <- result{page, err}
			}()
			p.wait(t)
			switch kind {
			case "remove":
				if err := (workspace.Store{Pool: db}).DeleteMember(ctx, f.od, f.w.ID, f.member.ID); err != nil {
					t.Fatal("roster read blocked removal")
				}
			case "role":
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal("role fixture")
				}
				defer tx.Rollback(context.Background())
				if maintainMember(ctx, tx, f.w.ID, f.member.ID, "promote") != nil || tx.Commit(ctx) != nil {
					t.Fatal("roster read blocked role change")
				}
			case "revoke":
				if (session.Store{Pool: db}).Revoke(ctx, f.md) != nil {
					t.Fatal("roster read blocked revoke")
				}
			case "disable":
				if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.member.ID); err != nil {
					t.Fatal("roster read blocked disable")
				}
			}
			p.resume()
			var previous result
			select {
			case previous = <-done:
			case <-ctx.Done():
				t.Fatal("roster snapshot did not finish")
			}
			if previous.err != nil || len(previous.page.Data) != 1 || previous.page.Pagination.NextAfter == nil {
				t.Fatal("authorized earlier snapshot lost", previous.err)
			}
			next, err := (workspace.Store{Pool: db}).ListMembers(ctx, f.md, f.w.ID, 1, *previous.page.Pagination.NextAfter)
			switch kind {
			case "remove":
				if !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("old cursor restored member access", err)
				}
			case "role":
				if err != nil || len(next.Data) != 1 || next.Data[0].UserID != f.member.ID || next.Data[0].Role != "owner" {
					t.Fatal("new snapshot kept old role", err)
				}
			default:
				if !errors.Is(err, session.ErrInvalid) {
					t.Fatal("new snapshot kept old identity", err)
				}
			}
		})
	}
}

func TestMemberMixedOperationsAndCleanup(t *testing.T) {
	db := workspaceDB(t)
	t.Run("member-first", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		f := membersFixture(t, db)
		p := newWorkspacePause(t)
		observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
			return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
				if stage == "members" {
					p.stop(ctx)
				}
			}}
		}}
		done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
		p.wait(t)
		// These operations must finish while the existing tenant mutation is held.
		if _, err := (workspace.Store{Pool: db}).Create(ctx, f.od, "parallel create"); err != nil {
			t.Fatal("workspace create lock conflict", err)
		}
		_, digest, err := session.NewTokens().New()
		if err != nil {
			t.Fatal("token fixture")
		}
		if _, err := (session.Store{Pool: db}).Create(ctx, user.Credentials{Public: f.owner, PasswordHash: password.DummyPHC}, digest, time.Hour); err != nil {
			t.Fatal("login session lock conflict", err)
		}
		if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
			t.Fatal("mixed cleanup", err)
		}
		if err := (workspace.Store{Pool: db}).DeleteMember(ctx, f.xd, f.z.ID, f.member.ID); err != nil {
			t.Fatal("unrelated tenant was serialized", err)
		}
		p.resume()
		awaitMember(t, done, nil)
	})
	t.Run("cleanup-first", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		f := membersFixture(t, db)
		p := newWorkspacePause(t)
		observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
			return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
				if stage == "candidate" {
					p.stop(ctx)
				}
			}}
		}}
		done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
		p.wait(t)
		if (session.Store{Pool: db}).Revoke(ctx, f.od) != nil {
			t.Fatal("cleanup revoke")
		}
		if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
			t.Fatal("cleanup first")
		}
		p.resume()
		awaitMember(t, done, session.ErrInvalid)
		if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
			t.Fatal("cleanup raced past identity gate")
		}
	})
}
