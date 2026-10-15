//go:build integration

package integration

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
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

// These wrappers execute production SQL against PG18. Only scheduling and
// injected transport outcomes are controlled; no alternate SQL is substituted.
type workspaceObservedDB struct {
	*pgxpool.Pool
	wrap     func(pgx.Tx) pgx.Tx
	beginErr error
	read     func(context.Context)
	pid      chan int
}

func (d workspaceObservedDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	tx, err := d.Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	if d.pid != nil {
		var pid int
		if err := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			tx.Rollback(ctx)
			return nil, err
		}
		d.pid <- pid
	}
	if d.wrap != nil {
		return d.wrap(tx), nil
	}
	return tx, nil
}
func (d workspaceObservedDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return workspaceObservedRow{Row: d.Pool.QueryRow(ctx, sql, args...), after: func() {
		if d.read != nil {
			d.read(ctx)
		}
	}}
}

func (d workspaceObservedDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := d.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &workspaceObservedRows{Rows: rows, after: func() {
		if d.read != nil {
			d.read(ctx)
		}
	}}, nil
}

type workspaceObservedRows struct {
	pgx.Rows
	after func()
	once  sync.Once
}

func (r *workspaceObservedRows) Scan(dest ...any) error {
	err := r.Rows.Scan(dest...)
	if err == nil {
		r.once.Do(r.after)
	}
	return err
}

type workspaceObservedRow struct {
	pgx.Row
	after func()
}

func (r workspaceObservedRow) Scan(dest ...any) error {
	err := r.Row.Scan(dest...)
	if err == nil && r.after != nil {
		r.after()
	}
	return err
}

type workspaceErrorRow struct{ err error }

func (r workspaceErrorRow) Scan(...any) error { return r.err }

type workspaceObservedTx struct {
	pgx.Tx
	after         func(context.Context, string)
	fail          string
	commitOutcome string
	commits       *atomic.Int32
}

func workspaceStage(sql string) string {
	switch {
	case strings.HasPrefix(sql, "SELECT user_id"):
		return "candidate"
	case strings.HasPrefix(sql, "SELECT status"):
		return "user"
	case strings.HasPrefix(sql, "SELECT revoked_at"):
		return "session"
	case strings.HasPrefix(sql, "INSERT INTO workspaces"):
		return "insert"
	}
	return "other"
}
func (tx workspaceObservedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	stage := workspaceStage(sql)
	if tx.fail == stage {
		return workspaceErrorRow{io.ErrUnexpectedEOF}
	}
	return workspaceObservedRow{Row: tx.Tx.QueryRow(ctx, sql, args...), after: func() {
		if tx.after != nil {
			tx.after(ctx, stage)
		}
	}}
}
func (tx workspaceObservedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.fail == "member" {
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "23514", Message: "SENSITIVE member failure"}
	}
	return tx.Tx.Exec(ctx, sql, args...)
}
func (tx workspaceObservedTx) Commit(ctx context.Context) error {
	if tx.commits != nil {
		tx.commits.Add(1)
	}
	if tx.after != nil {
		tx.after(ctx, "commit")
	}
	if tx.commitOutcome == "rollback" {
		if err := tx.Tx.Rollback(ctx); err != nil {
			return err
		}
		return io.ErrUnexpectedEOF
	}
	err := tx.Tx.Commit(ctx)
	if err == nil && tx.commitOutcome == "lost" {
		return io.ErrUnexpectedEOF
	}
	return err
}

type workspacePause struct {
	entered, release       chan struct{}
	enterOnce, releaseOnce sync.Once
}

func newWorkspacePause(t *testing.T) *workspacePause {
	p := &workspacePause{entered: make(chan struct{}), release: make(chan struct{})}
	t.Cleanup(p.resume)
	return p
}
func (p *workspacePause) stop(ctx context.Context) {
	p.enterOnce.Do(func() {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
		}
	})
}
func (p *workspacePause) resume() { p.releaseOnce.Do(func() { close(p.release) }) }
func (p *workspacePause) wait(t *testing.T) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("workspace scheduling barrier not reached")
	}
}
func waitWorkspaceBlock(t *testing.T, db *pgxpool.Pool, pid int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		var blocked bool
		if err := db.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1))>0`, pid).Scan(&blocked); err != nil {
			t.Fatal("lock observation")
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("expected PostgreSQL row lock wait was not observed")
		case <-time.After(time.Millisecond):
		}
	}
}

type workspaceCreateResult struct {
	w   workspace.Public
	err error
}

func startWorkspaceCreate(ctx context.Context, store workspace.Store, d session.Digest) <-chan workspaceCreateResult {
	ch := make(chan workspaceCreateResult, 1)
	go func() { w, err := store.Create(ctx, d, "atomic tenant"); ch <- workspaceCreateResult{w, err} }()
	return ch
}
func awaitWorkspace(t *testing.T, ch <-chan workspaceCreateResult) workspaceCreateResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(4 * time.Second):
		t.Fatal("workspace create did not terminate")
		return workspaceCreateResult{}
	}
}

func TestWorkspaceCreationLockOrdering(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"revoke", "disable"} {
		for _, winner := range []string{"change", "create"} {
			t.Run(kind+"-"+winner+"-first", func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				u, _, digest := workspaceIdentity(t, db, kind+winner+"@example.com")
				before := queryInt(t, db, `SELECT count(*) FROM workspaces`)
				pid := make(chan int, 1)
				pause := newWorkspacePause(t)
				var stages []string
				observed := workspaceObservedDB{Pool: db, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
					return workspaceObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						stages = append(stages, stage)
						if winner == "create" && stage == "insert" {
							pause.stop(ctx)
						}
					}}
				}}
				changeSQL := `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`
				if kind == "disable" {
					changeSQL = `UPDATE users SET status='disabled' WHERE id=$1`
				}
				if winner == "change" {
					tx, err := db.Begin(ctx)
					if err != nil {
						t.Fatal("state transaction")
					}
					defer tx.Rollback(ctx)
					if _, err = tx.Exec(ctx, changeSQL, u.ID); err != nil {
						t.Fatal("state mutation")
					}
					result := startWorkspaceCreate(ctx, workspace.Store{Pool: observed}, digest)
					waitWorkspaceBlock(t, db, <-pid)
					if err = tx.Commit(ctx); err != nil {
						t.Fatal("state commit")
					}
					if got := awaitWorkspace(t, result); !errors.Is(got.err, session.ErrInvalid) {
						t.Fatal("stale entry identity created workspace")
					}
					if queryInt(t, db, `SELECT count(*) FROM workspaces`) != before {
						t.Fatal("denied creation wrote rows")
					}
				} else {
					result := startWorkspaceCreate(ctx, workspace.Store{Pool: observed}, digest)
					<-pid
					pause.wait(t)
					// The first workspace INSERT is still uncommitted, no owner yet.
					if queryInt(t, db, `SELECT count(*) FROM workspaces`) != before {
						t.Fatal("partially published workspace")
					}
					conn, err := db.Acquire(ctx)
					if err != nil {
						t.Fatal("change connection")
					}
					defer conn.Release()
					var changePID int
					if conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&changePID) != nil {
						t.Fatal("change PID")
					}
					done := make(chan error, 1)
					go func() { _, err := conn.Exec(ctx, changeSQL, u.ID); done <- err }()
					waitWorkspaceBlock(t, db, changePID)
					pause.resume()
					got := awaitWorkspace(t, result)
					if got.err != nil {
						t.Fatal("protected create failed")
					}
					if err := <-done; err != nil {
						t.Fatal("waiting state change failed")
					}
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role='owner'`, got.w.ID, u.ID) != 1 {
						t.Fatal("owner not atomic")
					}
					if _, err := (workspace.Store{Pool: db}).Detail(ctx, digest, got.w.ID); !errors.Is(err, session.ErrInvalid) {
						t.Fatal("subsequent request ignored state change")
					}
					if strings.Join(stages, ",") != "candidate,user,session,insert,commit" {
						t.Fatal("User -> Session -> insertion protocol changed")
					}
				}
			})
		}
	}
}

func waitWorkspaceTime(t *testing.T, db *pgxpool.Pool, at time.Time) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		var passed bool
		if db.QueryRow(ctx, `SELECT statement_timestamp()>$1`, at).Scan(&passed) != nil {
			t.Fatal("database clock")
		}
		if passed {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("database time boundary")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestWorkspaceExpiryAndCleanupBoundaries(t *testing.T) {
	db := workspaceDB(t)
	for _, where := range []string{"before-insert", "after-insert"} {
		t.Run(where, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			u, _, digest := workspaceIdentity(t, db, where+"@example.com")
			var expiry time.Time
			if db.QueryRow(ctx, `UPDATE sessions SET expires_at=statement_timestamp()+interval '300ms' WHERE token_hash=$1 RETURNING expires_at`, digest[:]).Scan(&expiry) != nil {
				t.Fatal("expiry fixture")
			}
			pause := newWorkspacePause(t)
			observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				return workspaceObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
					if where == "after-insert" && stage == "insert" {
						pause.stop(ctx)
					}
				}}
			}}
			var result <-chan workspaceCreateResult
			if where == "before-insert" {
				blocker, err := db.Begin(ctx)
				if err != nil {
					t.Fatal("expiry blocker")
				}
				defer blocker.Rollback(ctx)
				if _, err = blocker.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, u.ID); err != nil {
					t.Fatal("user blocker")
				}
				observed.pid = make(chan int, 1)
				result = startWorkspaceCreate(ctx, workspace.Store{Pool: observed}, digest)
				waitWorkspaceBlock(t, db, <-observed.pid)
				waitWorkspaceTime(t, db, expiry)
				if blocker.Commit(ctx) != nil {
					t.Fatal("expiry blocker release")
				}
			} else {
				result = startWorkspaceCreate(ctx, workspace.Store{Pool: observed}, digest)
				pause.wait(t)
				waitWorkspaceTime(t, db, expiry)
				if n, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil || n != 0 {
					t.Fatal("cleanup must skip locked expired session")
				}
				pause.resume()
			}
			got := awaitWorkspace(t, result)
			if where == "before-insert" {
				if !errors.Is(got.err, session.ErrInvalid) {
					t.Fatal("pre-wait statement time authorized expired session")
				}
			} else if got.err != nil {
				t.Fatal("post-gate expiry incorrectly rolled back")
			}
			if _, err := (workspace.Store{Pool: db}).List(ctx, digest, 50, ""); !errors.Is(err, session.ErrInvalid) {
				t.Fatal("expired session read")
			}
			if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
				t.Fatal("post-lock cleanup")
			}
			if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, digest[:]) != 0 {
				t.Fatal("expired row not cleaned")
			}
		})
	}
	// Cleanup may physically remove a revoked session before creation.
	_, _, digest := workspaceIdentity(t, db, "cleaned-before-create@example.com")
	if (session.Store{Pool: db}).Revoke(context.Background(), digest) != nil {
		t.Fatal("revoke fixture")
	}
	if _, err := (session.Store{Pool: db}).Cleanup(context.Background()); err != nil {
		t.Fatal("cleanup fixture")
	}
	if _, err := (workspace.Store{Pool: db}).Create(context.Background(), digest, "no row"); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("deleted session authenticated")
	}
}

func TestWorkspaceReadSnapshotMembershipAndIdentityChanges(t *testing.T) {
	db := workspaceDB(t)
	for _, kind := range []string{"remove", "role", "revoke", "disable"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			u, _, digest := workspaceIdentity(t, db, "read-"+kind+"@example.com")
			store := workspace.Store{Pool: db}
			w, err := store.Create(ctx, digest, "snapshot")
			if err != nil {
				t.Fatal("read fixture")
			}
			pause := newWorkspacePause(t)
			observed := workspaceObservedDB{Pool: db, read: pause.stop}
			result := make(chan workspaceCreateResult, 1)
			go func() {
				got, err := (workspace.Store{Pool: observed}).Detail(ctx, digest, w.ID)
				result <- workspaceCreateResult{got, err}
			}()
			pause.wait(t)
			sql := `DELETE FROM workspace_members WHERE user_id=$1`
			switch kind {
			case "role":
				sql = `UPDATE workspace_members SET role='member' WHERE user_id=$1`
			case "revoke":
				sql = `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`
			case "disable":
				sql = `UPDATE users SET status='disabled' WHERE id=$1`
			}
			if _, err := db.Exec(ctx, sql, u.ID); err != nil {
				t.Fatal("read must not block state mutation")
			}
			pause.resume()
			previous := awaitWorkspace(t, result)
			if previous.err != nil || previous.w.Role != "owner" {
				t.Fatal("previous valid snapshot must finish")
			}
			current, err := store.Detail(ctx, digest, w.ID)
			switch kind {
			case "remove":
				if !errors.Is(err, workspace.ErrNotFound) {
					t.Fatal("new snapshot ignored removal")
				}
				page, err := store.List(ctx, digest, 50, "")
				if err != nil || len(page.Data) != 0 {
					t.Fatal("new empty membership list")
				}
			case "role":
				if err != nil || current.Role != "member" {
					t.Fatal("role snapshot stale")
				}
			default:
				if !errors.Is(err, session.ErrInvalid) {
					t.Fatal("new snapshot ignored invalid identity")
				}
			}
		})
	}
}

func TestWorkspaceTransactionFaultsAndUnknownCommit(t *testing.T) {
	db := workspaceDB(t)
	_, _, digest := workspaceIdentity(t, db, "faults@example.com")
	// Force the SECOND production INSERT to fail inside PostgreSQL itself.
	// This constraint exists only in the guarded disposable schema fixture.
	if _, err := db.Exec(context.Background(), `ALTER TABLE workspace_members ADD CONSTRAINT workspace_member_failure_fixture CHECK (role='member') NOT VALID`); err != nil {
		t.Fatal("owner check failure fixture")
	}
	_, createErr := (workspace.Store{Pool: db}).Create(context.Background(), digest, "must roll back")
	if _, err := db.Exec(context.Background(), `ALTER TABLE workspace_members DROP CONSTRAINT workspace_member_failure_fixture`); err != nil {
		t.Fatal("owner check fixture restore")
	}
	if !errors.Is(createErr, user.ErrInternal) || queryInt(t, db, `SELECT count(*) FROM workspaces`) != 0 || queryInt(t, db, `SELECT count(*) FROM workspace_members`) != 0 {
		t.Fatal("real PostgreSQL owner constraint failure left a partial workspace")
	}
	for _, stage := range []string{"begin", "candidate", "user", "session", "insert", "member", "rollback", "lost"} {
		t.Run(stage, func(t *testing.T) {
			before := queryInt(t, db, `SELECT count(*) FROM workspaces`)
			var commits atomic.Int32
			observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				return workspaceObservedTx{Tx: tx, fail: stage, commitOutcome: stage, commits: &commits}
			}}
			if stage == "begin" {
				observed.beginErr = io.ErrUnexpectedEOF
			}
			_, err := (workspace.Store{Pool: observed}).Create(context.Background(), digest, "faults")
			if err == nil {
				t.Fatal("injected failure produced success")
			}
			want := before
			if stage == "lost" {
				want++
			}
			if queryInt(t, db, `SELECT count(*) FROM workspaces`) != want || queryInt(t, db, `SELECT count(*) FROM workspaces w LEFT JOIN workspace_members m ON m.workspace_id=w.id WHERE m.workspace_id IS NULL`) != 0 {
				t.Fatal("transaction fault left partial or duplicate data")
			}
			if stage == "lost" || stage == "rollback" {
				if commits.Load() != 1 {
					t.Fatal("commit automatically retried")
				}
			} else if commits.Load() != 0 {
				t.Fatal("failure proceeded to commit")
			}
			if stage == "member" && !errors.Is(err, user.ErrInternal) {
				t.Fatal("unexpected constraint classification")
			}
			page, listErr := (workspace.Store{Pool: db}).List(context.Background(), digest, 50, "")
			if listErr != nil || len(page.Data) != want {
				t.Fatal("unknown commit storage recovery")
			}
		})
	}
	// Propagated cancellation after protected validation must not insert or
	// return an open transaction to the pool.
	ctx, cancel := context.WithCancel(context.Background())
	observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return workspaceObservedTx{Tx: tx, after: func(_ context.Context, stage string) {
			if stage == "session" {
				cancel()
			}
		}}
	}}
	before := queryInt(t, db, `SELECT count(*) FROM workspaces`)
	if _, err := (workspace.Store{Pool: observed}).Create(ctx, digest, "canceled"); !errors.Is(err, user.ErrCanceled) {
		t.Fatal("cancel not propagated")
	}
	if queryInt(t, db, `SELECT count(*) FROM workspaces`) != before {
		t.Fatal("canceled creation wrote")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`) != 0 {
		t.Fatal("transaction leaked into pool")
	}
}
