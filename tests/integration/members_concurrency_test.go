//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

type memberObservedTx struct {
	pgx.Tx
	after         func(context.Context, string)
	commitOutcome string
	commits       *atomic.Int32
	faultStage    string
	fault         error
}

func memberStage(sql string) string {
	switch {
	case strings.HasPrefix(sql, "SELECT w.id"):
		return "workspace"
	case strings.HasPrefix(sql, "SELECT role"):
		return "scope"
	case strings.HasPrefix(sql, "WITH live_identity"):
		return "gate"
	default:
		return workspaceStage(sql)
	}
}
func (tx memberObservedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.faultStage == memberStage(sql) {
		return workspaceErrorRow{tx.fault}
	}
	return workspaceObservedRow{Row: tx.Tx.QueryRow(ctx, sql, args...), after: func() {
		if tx.after != nil {
			tx.after(ctx, memberStage(sql))
		}
	}}
}

type memberObservedRows struct {
	pgx.Rows
	done func()
}

func (r *memberObservedRows) Next() bool {
	next := r.Rows.Next()
	if !next && r.Rows.Err() == nil && r.done != nil {
		f := r.done
		r.done = nil
		f()
	}
	return next
}
func (tx memberObservedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &memberObservedRows{Rows: rows, done: func() {
		if tx.after != nil {
			tx.after(ctx, "members")
		}
	}}, nil
}
func (tx memberObservedTx) Commit(ctx context.Context) error {
	return (workspaceObservedTx{Tx: tx.Tx, after: tx.after, commitOutcome: tx.commitOutcome, commits: tx.commits}).Commit(ctx)
}
func startMemberDelete(ctx context.Context, s workspace.Store, d session.Digest, wid, target string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- s.DeleteMember(ctx, d, wid, target) }()
	return done
}
func awaitMember(t *testing.T, done <-chan error, want error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("member transaction got %v want %v", err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("member transaction did not terminate")
	}
}
func TestMemberWriteLockOrder(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	p := newWorkspacePause(t)
	pid := make(chan int, 1)
	var stages []string
	observed := workspaceObservedDB{Pool: db, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
		return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			stages = append(stages, stage)
			if stage == "members" {
				p.stop(ctx)
			}
		}}
	}}
	done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
	p.wait(t)
	if strings.Join(stages, ",") != "candidate,user,session,workspace,scope,members" {
		t.Fatal("member lock sequence", stages)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal("blocking fixture")
	}
	defer tx.Rollback(context.Background())
	waitDone := make(chan error, 1)
	go func() {
		_, err := tx.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID)
		waitDone <- err
	}()
	// Observe the actual membership row wait, not elapsed time.
	var waiter int
	if err := tx.Conn().PgConn().PID(); err != 0 {
		waiter = int(err)
	}
	waitWorkspaceBlock(t, db, waiter)
	p.resume()
	awaitMember(t, done, nil)
	awaitMember(t, waitDone, nil)
}

func addFixtureOwner(t *testing.T, tx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, f memberFixture) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.w.ID, f.other.ID); err != nil {
		t.Fatal("second owner fixture")
	}
}
func TestMemberConcurrentOwnerAndExitRaces(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"two-exits", "two-exits-rollback", "two-exits-lost", "mutual-owner", "duplicate-delete", "owner-exit-race"} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-reverse-%v", kind, reverse), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				f := membersFixture(t, db)
				addFixtureOwner(t, db, f)
				a, b := f.od, f.xd
				at, bt := f.owner.ID, f.other.ID
				var firstWant, secondWant error
				switch kind {
				case "two-exits":
					secondWant = workspace.ErrLastOwner
				case "two-exits-rollback":
					firstWant = user.ErrUnavailable
				case "two-exits-lost":
					firstWant = user.ErrUnavailable
					secondWant = workspace.ErrLastOwner
				case "mutual-owner":
					at, bt = bt, at
					firstWant = workspace.ErrMemberForbidden
					secondWant = workspace.ErrMemberForbidden
				case "duplicate-delete":
					at, bt = f.member.ID, f.member.ID
				case "owner-exit-race":
					b = f.md
					at, bt = f.member.ID, f.member.ID
					secondWant = workspace.ErrNotFound
				}
				if reverse {
					a, b = b, a
					at, bt = bt, at
					if kind == "owner-exit-race" {
						secondWant = nil
					}
				}
				pause := newWorkspacePause(t)
				observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
					outcome := ""
					if kind == "two-exits-rollback" {
						outcome = "rollback"
					}
					if kind == "two-exits-lost" {
						outcome = "lost"
					}
					return memberObservedTx{Tx: tx, commitOutcome: outcome, after: func(ctx context.Context, stage string) {
						if stage == "members" {
							pause.stop(ctx)
						}
					}}
				}}
				first := startMemberDelete(ctx, workspace.Store{Pool: observed}, a, f.w.ID, at)
				pause.wait(t)
				pid := make(chan int, 1)
				second := startMemberDelete(ctx, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, b, f.w.ID, bt)
				waitWorkspaceBlock(t, db, <-pid)
				pause.resume()
				awaitMember(t, first, firstWant)
				awaitMember(t, second, secondWant)
				wantOwners := 2
				if strings.HasPrefix(kind, "two-exits") {
					wantOwners = 1
				}
				if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role='owner'`, f.w.ID) != wantOwners {
					t.Fatal("concurrent final owner invariant")
				}
				if kind == "duplicate-delete" || kind == "owner-exit-race" {
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 0 {
						t.Fatal("concurrent member still present")
					}
				}
			})
		}
	}
}

// Maintenance fixtures obey the same Workspace -> Membership sequence. They
// exercise future role/account integration boundaries, not public role APIs.
func maintainMember(ctx context.Context, tx pgx.Tx, wid, uid, kind string) error {
	if _, err := tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, wid); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 ORDER BY user_id FOR UPDATE`, wid, uid); err != nil {
		return err
	}
	sql := `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`
	if kind == "remove" {
		sql = `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`
	}
	if kind == "promote" {
		sql = `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`
	}
	_, err := tx.Exec(ctx, sql, wid, uid)
	return err
}
func TestMemberPermissionChangeOrdering(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"demote", "remove", "promote"} {
		for _, winner := range []string{"change", "member"} {
			t.Run(kind+"-"+winner, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				f := membersFixture(t, db)
				addFixtureOwner(t, db, f)
				changed := f.owner.ID
				if kind == "promote" {
					changed = f.member.ID
				}
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal("maintenance tx")
				}
				defer tx.Rollback(context.Background())
				if winner == "change" {
					if err = maintainMember(ctx, tx, f.w.ID, changed, kind); err != nil {
						t.Fatal("maintenance change")
					}
					pid := make(chan int, 1)
					done := startMemberDelete(ctx, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.od, f.w.ID, f.member.ID)
					waitWorkspaceBlock(t, db, <-pid)
					if tx.Commit(ctx) != nil {
						t.Fatal("maintenance commit")
					}
					want := workspace.ErrMemberForbidden
					if kind == "remove" {
						want = workspace.ErrNotFound
					}
					awaitMember(t, done, want)
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
						t.Fatal("stale actor/target privilege")
					}
				} else {
					pause := newWorkspacePause(t)
					observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
						return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
							if stage == "members" {
								pause.stop(ctx)
							}
						}}
					}}
					done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
					pause.wait(t)
					pid := int(tx.Conn().PgConn().PID())
					changedDone := make(chan error, 1)
					go func() { changedDone <- maintainMember(ctx, tx, f.w.ID, changed, kind) }()
					waitWorkspaceBlock(t, db, pid)
					pause.resume()
					awaitMember(t, done, nil)
					awaitMember(t, changedDone, nil)
					if tx.Commit(ctx) != nil {
						t.Fatal("later maintenance commit")
					}
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 0 {
						t.Fatal("later change restored deleted relation")
					}
				}
			})
		}
	}
}

func TestMemberIdentityChangeOrdering(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"revoke", "disable"} {
		for _, winner := range []string{"change", "member"} {
			t.Run(kind+"-"+winner, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				f := membersFixture(t, db)
				tx, err := db.Begin(ctx)
				if err != nil {
					t.Fatal("identity tx")
				}
				defer tx.Rollback(context.Background())
				change := func() error {
					if kind == "disable" {
						if _, err := tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); err != nil {
							return err
						}
					}
					_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`, f.owner.ID)
					return err
				}
				if winner == "change" {
					if change() != nil {
						t.Fatal("identity fixture")
					}
					pid := make(chan int, 1)
					done := startMemberDelete(ctx, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.od, f.w.ID, f.member.ID)
					waitWorkspaceBlock(t, db, <-pid)
					if tx.Commit(ctx) != nil {
						t.Fatal("identity commit")
					}
					awaitMember(t, done, session.ErrInvalid)
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
						t.Fatal("identity change lost race")
					}
				} else {
					pause := newWorkspacePause(t)
					observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
						return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
							if stage == "members" {
								pause.stop(ctx)
							}
						}}
					}}
					done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
					pause.wait(t)
					pid := int(tx.Conn().PgConn().PID())
					changed := make(chan error, 1)
					go func() { changed <- change() }()
					waitWorkspaceBlock(t, db, pid)
					pause.resume()
					awaitMember(t, done, nil)
					awaitMember(t, changed, nil)
					if tx.Commit(ctx) != nil {
						t.Fatal("later identity commit")
					}
				}
				if _, err := (workspace.Store{Pool: db}).ListMembers(ctx, f.od, f.w.ID, 50, ""); !errors.Is(err, session.ErrInvalid) {
					t.Fatal("fresh identity reused old grant", err)
				}
			})
		}
	}
}

func waitMemberExpiry(t *testing.T, db *pgxpool.Pool, d session.Digest) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		var expired bool
		if err := db.QueryRow(ctx, `SELECT expires_at<=statement_timestamp() FROM sessions WHERE token_hash=$1`, d[:]).Scan(&expired); err != nil {
			t.Fatal("database clock observation")
		}
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("expiry not reached")
		case <-time.After(time.Millisecond):
		}
	}
}
func expireMemberSoon(t *testing.T, db *pgxpool.Pool, d session.Digest) {
	t.Helper()
	if _, err := db.Exec(context.Background(), `UPDATE sessions SET created_at=statement_timestamp()-interval '1h',expires_at=statement_timestamp()+interval '300 milliseconds' WHERE token_hash=$1`, d[:]); err != nil {
		t.Fatal("expiry fixture")
	}
}
func TestMemberExpiryAfterEveryLockWait(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"user", "session", "workspace", "membership"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			f := membersFixture(t, db)
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal("expiry blocker")
			}
			defer tx.Rollback(context.Background())
			sql := `SELECT id FROM users WHERE id=$1 FOR UPDATE`
			args := []any{f.owner.ID}
			switch kind {
			case "session":
				sql = `SELECT id FROM sessions WHERE token_hash=$1 FOR UPDATE`
				args = []any{f.od[:]}
			case "workspace":
				sql = `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`
				args = []any{f.w.ID}
			case "membership":
				sql = `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`
				args = []any{f.w.ID, f.member.ID}
			}
			if _, err = tx.Exec(ctx, sql, args...); err != nil {
				t.Fatal("expiry lock")
			}
			// Session expiry must be set before a session blocker holds that same row.
			if kind == "session" {
				if _, err = tx.Exec(ctx, `UPDATE sessions SET created_at=statement_timestamp()-interval '1h',expires_at=statement_timestamp()+interval '300 milliseconds' WHERE token_hash=$1`, f.od[:]); err != nil {
					t.Fatal("session expiry update")
				}
			} else {
				expireMemberSoon(t, db, f.od)
			}
			pid := make(chan int, 1)
			done := startMemberDelete(ctx, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.od, f.w.ID, f.member.ID)
			waitWorkspaceBlock(t, db, <-pid)
			if kind == "session" {
				// The expiry update is uncommitted: read the blocker transaction's clock,
				// not a different connection's older expires_at snapshot.
				for {
					var expired bool
					if tx.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_hash=$1`, f.od[:]).Scan(&expired) != nil {
						t.Fatal("locked session time")
					}
					if expired {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal("session expiry wait")
					case <-time.After(time.Millisecond):
					}
				}
			} else {
				waitMemberExpiry(t, db, f.od)
			}
			if tx.Commit(ctx) != nil {
				t.Fatal("expiry blocker release")
			}
			awaitMember(t, done, session.ErrInvalid)
			if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
				t.Fatal("pre-wait timestamp authorized deletion")
			}
		})
	}
}
func TestMemberPostGateExpiryAndCleanup(t *testing.T) {
	db := workspaceDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	f := membersFixture(t, db)
	expireMemberSoon(t, db, f.od)
	pause := newWorkspacePause(t)
	observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "gate" {
				pause.stop(ctx)
			}
		}}
	}}
	done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID)
	pause.wait(t)
	waitMemberExpiry(t, db, f.od)
	if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
		t.Fatal("cleanup skip locked")
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, f.od[:]) != 1 {
		t.Fatal("cleanup did not skip protected session")
	}
	pause.resume()
	awaitMember(t, done, nil)
	if _, err := (session.Store{Pool: db}).Lookup(ctx, f.od); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("post gate expiry renewed identity")
	}
	if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
		t.Fatal("cleanup after member commit")
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, f.od[:]) != 0 {
		t.Fatal("cleanup no longer blocked")
	}
}
