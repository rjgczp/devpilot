//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func TestWorkspaceListSnapshotAndCursorChanges(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"remove", "role", "revoke", "disable"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			u, _, digest := workspaceIdentity(t, db, "list-"+kind+"@example.com")
			store := workspace.Store{Pool: db}
			x, err := store.Create(ctx, digest, "page one")
			if err != nil {
				t.Fatal("list fixture")
			}
			y, err := store.Create(ctx, digest, "page two")
			if err != nil {
				t.Fatal("list fixture")
			}
			if x.ID > y.ID {
				x, y = y, x
			}
			pause := newWorkspacePause(t)
			observed := workspaceObservedDB{Pool: db, read: pause.stop}
			type result struct {
				page workspace.Page
				err  error
			}
			done := make(chan result, 1)
			go func() {
				page, err := (workspace.Store{Pool: observed}).List(ctx, digest, 1, "")
				done <- result{page, err}
			}()
			pause.wait(t)
			sql := `DELETE FROM workspace_members WHERE user_id=$1 AND workspace_id=$2`
			switch kind {
			case "role":
				sql = `UPDATE workspace_members SET role='member' WHERE user_id=$1 AND workspace_id=$2`
			case "revoke":
				sql = `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1 AND $2::uuid IS NOT NULL`
			case "disable":
				sql = `UPDATE users SET status='disabled' WHERE id=$1 AND $2::uuid IS NOT NULL`
			}
			if _, err := db.Exec(ctx, sql, u.ID, y.ID); err != nil {
				t.Fatal("list read locked state changes")
			}
			pause.resume()
			var previous result
			select {
			case previous = <-done:
			case <-ctx.Done():
				t.Fatal("list did not finish")
			}
			if previous.err != nil || len(previous.page.Data) != 1 || previous.page.Data[0].ID != x.ID || previous.page.Pagination.NextAfter == nil {
				t.Fatal("previous list snapshot was changed")
			}
			next, err := store.List(ctx, digest, 1, *previous.page.Pagination.NextAfter)
			switch kind {
			case "remove":
				if err != nil || len(next.Data) != 0 || next.Pagination.NextAfter != nil {
					t.Fatal("old cursor restored removed membership")
				}
			case "role":
				if err != nil || len(next.Data) != 1 || next.Data[0].Role != "member" {
					t.Fatal("old cursor kept stale role")
				}
			default:
				if !errors.Is(err, session.ErrInvalid) {
					t.Fatal("old cursor kept stale identity")
				}
			}
		})
	}
}
