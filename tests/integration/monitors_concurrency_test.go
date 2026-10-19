//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

type monitorObservedTx struct {
	pgx.Tx
	after         func(context.Context, string)
	outcome, fail string
	commits       *atomic.Int32
}

func monitorStage(sql string) string {
	if strings.HasPrefix(sql, "SELECT id::text FROM projects") {
		return "project"
	}
	if strings.HasPrefix(sql, "SELECT id::text,name,url,method") {
		return "monitor"
	}
	if strings.HasPrefix(sql, "SELECT role") {
		return "membership"
	}
	return memberStage(sql)
}
func (tx monitorObservedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	stage := monitorStage(sql)
	if stage == tx.fail {
		return workspaceErrorRow{io.ErrUnexpectedEOF}
	}
	return workspaceObservedRow{Row: tx.Tx.QueryRow(ctx, sql, args...), after: func() {
		if tx.after != nil {
			tx.after(ctx, stage)
		}
	}}
}
func (tx monitorObservedTx) Commit(ctx context.Context) error {
	return (workspaceObservedTx{Tx: tx.Tx, commitOutcome: tx.outcome, commits: tx.commits}).Commit(ctx)
}

type monitorResult struct {
	p   monitor.Public
	err error
}

func startMonitor(ctx context.Context, s monitor.Store, d session.Digest, p monitor.Public, update bool) <-chan monitorResult {
	ch := make(chan monitorResult, 1)
	go func() {
		var r monitorResult
		if update {
			r.p, r.err = s.Update(ctx, d, p.WorkspaceID, p.ProjectID, p.ID, monitorInput("edited"), monitor.ETag(p))
		} else {
			r.p, r.err = s.Create(ctx, d, p.WorkspaceID, p.ProjectID, monitorInput("created"))
		}
		ch <- r
	}()
	return ch
}
func awaitMonitor(t *testing.T, ch <-chan monitorResult, want error) monitor.Public {
	t.Helper()
	select {
	case r := <-ch:
		monitorWant(t, r.err, want)
		return r.p
	case <-time.After(5 * time.Second):
		t.Fatal("monitor did not complete")
		return monitor.Public{}
	}
}
func TestMonitorConcurrentVersions(t *testing.T) {
	db := workspaceDB(t)
	for _, same := range []bool{false, true} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			f, p := monitorFixture(t, db)
			if _, err := db.Exec(context.Background(), `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			pause := newWorkspacePause(t)
			first := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
					if stage == "gate" {
						pause.stop(ctx)
					}
				}}
			}}}
			a := startMonitor(ctx, first, f.od, p, true)
			pause.wait(t)
			pid := make(chan int, 1)
			second := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}
			b := make(chan monitorResult, 1)
			go func() {
				v := monitorInput("other-field")
				if !same {
					v = monitor.Input{Method: ptrMonitorString("GET")}
				}
				got, err := second.Update(ctx, f.md, p.WorkspaceID, p.ProjectID, p.ID, v, monitor.ETag(p))
				b <- monitorResult{got, err}
			}()
			waitWorkspaceBlock(t, db, <-pid)
			pause.resume()
			got := awaitMonitor(t, a, nil)
			awaitMonitor(t, b, monitor.ErrPreconditionFailed)
			if got.Version != "2" {
				t.Fatal("version")
			}
			_, err := (monitor.Store{Pool: db}).Update(ctx, f.md, p.WorkspaceID, p.ProjectID, p.ID, monitorInput("replay"), monitor.ETag(p))
			monitorWant(t, err, monitor.ErrPreconditionFailed)
		})
	}
}
func TestMonitorExpiryAfterEveryProtectiveWait(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		for _, stage := range []string{"user", "session", "workspace", "membership", "project", "monitor", "post-gate"} {
			if !update && stage == "monitor" {
				continue
			}
			t.Run(fmt.Sprintf("update%v-%s", update, stage), func(t *testing.T) {
				f, p := monitorFixture(t, db)
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				var expiry time.Time
				if db.QueryRow(ctx, `UPDATE sessions SET expires_at=statement_timestamp()+interval '150ms' WHERE token_hash=$1 RETURNING expires_at`, f.od[:]).Scan(&expiry) != nil {
					t.Fatal("expiry")
				}
				pause := newWorkspacePause(t)
				pid := make(chan int, 1)
				store := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
					return monitorObservedTx{Tx: tx, after: func(ctx context.Context, s string) {
						if stage == "post-gate" && s == "gate" {
							pause.stop(ctx)
						}
					}}
				}}}
				var ch <-chan monitorResult
				if stage == "post-gate" {
					ch = startMonitor(ctx, store, f.od, p, update)
					<-pid
					pause.wait(t)
					waitWorkspaceTime(t, db, expiry)
					if _, err := (session.Store{Pool: db}).Cleanup(ctx); err != nil {
						t.Fatal(err)
					}
					if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, f.od[:]) != 1 {
						t.Fatal("cleanup deleted protected Session")
					}
					pause.resume()
					awaitMonitor(t, ch, nil)
				} else {
					blocker, err := db.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer blocker.Rollback(ctx)
					sql := `SELECT id FROM users WHERE id=$1 FOR UPDATE`
					arg := any(f.owner.ID)
					switch stage {
					case "session":
						sql = `SELECT id FROM sessions WHERE token_hash=$1 FOR UPDATE`
						arg = f.od[:]
					case "workspace":
						sql = `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`
						arg = f.w.ID
					case "membership":
						sql = `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND role='owner' FOR UPDATE`
						arg = f.w.ID
					case "project":
						sql = `SELECT id FROM projects WHERE id=$1 FOR UPDATE`
						arg = p.ProjectID
					case "monitor":
						sql = `SELECT id FROM http_monitors WHERE id=$1 FOR UPDATE`
						arg = p.ID
					}
					if _, err = blocker.Exec(ctx, sql, arg); err != nil {
						t.Fatal(err)
					}
					ch = startMonitor(ctx, store, f.od, p, update)
					waitWorkspaceBlock(t, db, <-pid)
					waitWorkspaceTime(t, db, expiry)
					if blocker.Commit(ctx) != nil {
						t.Fatal("release")
					}
					awaitMonitor(t, ch, session.ErrInvalid)
				}
			})
		}
	}
}
func TestMonitorIdentityAndMembershipOrderings(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		for _, kind := range []string{"revoke", "disable", "role", "exit", "remove", "accept"} {
			for _, monitorFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("update%v-%s-monitorfirst%v", update, kind, monitorFirst), func(t *testing.T) {
					f, p := monitorFixture(t, db)
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					if kind == "exit" || kind == "role" {
						if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
							t.Fatal(err)
						}
					}
					var iid string
					if kind == "accept" {
						i, err := (workspace.Store{Pool: db}).CreateInvitation(ctx, f.od, f.w.ID, f.other.ID)
						if err != nil {
							t.Fatal(err)
						}
						iid = i.ID
					}
					pause := newWorkspacePause(t)
					monitorPID := make(chan int, 1)
					changePID := make(chan int, 1)
					ps := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: monitorPID, wrap: func(tx pgx.Tx) pgx.Tx {
						return monitorObservedTx{Tx: tx, after: func(ctx context.Context, s string) {
							if monitorFirst && s == "gate" {
								pause.stop(ctx)
							}
						}}
					}}}
					change := func() error {
						if kind == "exit" || kind == "remove" || kind == "accept" {
							ws := workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: changePID, wrap: func(tx pgx.Tx) pgx.Tx {
								return invitationObservedTx{Tx: tx, after: func(ctx context.Context, s string) {
									if !monitorFirst && s == "workspace" {
										pause.stop(ctx)
									}
								}}
							}}}
							if kind == "accept" {
								_, err := ws.ActOnInvitation(ctx, f.xd, iid, "accept")
								return err
							}
							target, d, cond := f.member.ID, f.od, f.condition(f.w.ID, f.member.ID)
							if kind == "exit" {
								target, d, cond = f.owner.ID, f.od, f.condition(f.w.ID, f.owner.ID)
							}
							return ws.DeleteMember(ctx, d, f.w.ID, target, cond)
						}
						tx, err := db.Begin(ctx)
						if err != nil {
							return err
						}
						defer tx.Rollback(ctx)
						var pid int
						if err = tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
							return err
						}
						changePID <- pid
						// Role maintenance follows User -> Workspace -> Membership; no public role API.
						if kind == "role" {
							if _, err = tx.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR SHARE`, f.owner.ID); err != nil {
								return err
							}
							if _, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID); err != nil {
								return err
							}
						}
						sql := `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`
						args := []any{f.owner.ID}
						if kind == "disable" {
							sql = `UPDATE users SET status='disabled' WHERE id=$1`
						}
						if kind == "role" {
							sql = `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`
							args = []any{f.w.ID, f.owner.ID}
						}
						if _, err = tx.Exec(ctx, sql, args...); err != nil {
							return err
						}
						if !monitorFirst {
							pause.stop(ctx)
						}
						return tx.Commit(ctx)
					}
					changes := make(chan error, 1)
					var action <-chan monitorResult
					if monitorFirst {
						action = startMonitor(ctx, ps, f.od, p, update)
						<-monitorPID
						pause.wait(t)
						go func() { changes <- change() }()
						waitWorkspaceBlock(t, db, <-changePID)
					} else {
						go func() { changes <- change() }()
						<-changePID
						pause.wait(t)
						action = startMonitor(ctx, ps, f.od, p, update)
						waitWorkspaceBlock(t, db, <-monitorPID)
					}
					pause.resume()
					var want error
					if !monitorFirst {
						switch kind {
						case "revoke", "disable":
							want = session.ErrInvalid
						case "role":
							want = monitor.ErrForbidden
						case "exit":
							want = monitor.ErrNotFound
						}
					}
					awaitMonitor(t, action, want)
					awaitMember(t, changes, nil)
				})
			}
		}
	}
}
func TestMonitorFaultAtomicityAndTenantIndependence(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, update := range []bool{false, true} {
		for _, fault := range []string{"candidate", "user", "session", "workspace", "membership", "project", "monitor", "gate", "rollback", "lost"} {
			if !update && fault == "monitor" {
				continue
			}
			t.Run(fmt.Sprintf("update%v-%s", update, fault), func(t *testing.T) {
				f, p := monitorFixture(t, db)
				var commits atomic.Int32
				observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
					return monitorObservedTx{Tx: tx, fail: fault, outcome: fault, commits: &commits}
				}}
				before := queryInt(t, db, `SELECT count(*) FROM http_monitors`)
				awaitMonitor(t, startMonitor(ctx, monitor.Store{Pool: observed}, f.od, p, update), user.ErrUnavailable)
				want := before
				if fault == "lost" && !update {
					want++
				}
				if queryInt(t, db, `SELECT count(*) FROM http_monitors`) != want {
					t.Fatal("partial/duplicate creation")
				}
				current, err := (monitor.Store{Pool: db}).Detail(ctx, f.od, p.WorkspaceID, p.ProjectID, p.ID)
				if err != nil {
					t.Fatal(err)
				}
				wantVersion := "1"
				if update && fault == "lost" {
					wantVersion = "2"
				}
				if current.Version != wantVersion {
					t.Fatal("rollback/lost outcome", current)
				}
				wantCommits := int32(0)
				if fault == "lost" || fault == "rollback" {
					wantCommits = 1
				}
				if commits.Load() != wantCommits {
					t.Fatal("automatic commit replay")
				}
			})
		}
	}
	t.Run("independent-tenants", func(t *testing.T) {
		f, p := monitorFixture(t, db)
		otherProject, e := (project.Store{Pool: db}).Create(ctx, f.xd, f.z.ID, projectInput("independent"))
		if e != nil {
			t.Fatal(e)
		}
		pause := newWorkspacePause(t)
		s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
			return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
				if stage == "gate" {
					pause.stop(ctx)
				}
			}}
		}}}
		ch := startMonitor(ctx, s, f.od, p, true)
		pause.wait(t)
		bctx, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		if _, err := (monitor.Store{Pool: db}).Create(bctx, f.xd, f.z.ID, otherProject.ID, monitorInput("independent")); err != nil {
			t.Fatal("global lock", err)
		}
		if _, err := (monitor.Store{Pool: db}).Detail(bctx, f.md, f.w.ID, p.ProjectID, p.ID); err != nil {
			t.Fatal("read blocked by writer", err)
		}
		pause.resume()
		awaitMonitor(t, ch, nil)
	})
	if queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`) != 0 {
		t.Fatal("transaction leak")
	}
}
func TestMonitorReadSnapshotAndLateCommit(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, p := monitorFixture(t, db)
	pause := newWorkspacePause(t)
	s := monitor.Store{Pool: workspaceObservedDB{Pool: db, read: pause.stop}}
	done := make(chan monitorResult, 1)
	go func() { got, err := s.Detail(ctx, f.md, f.w.ID, p.ProjectID, p.ID); done <- monitorResult{got, err} }()
	pause.wait(t)
	if err := (workspace.Store{Pool: db}).DeleteMember(ctx, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil {
		t.Fatal(err)
	}
	pause.resume()
	awaitMonitor(t, done, nil)
	_, err := (monitor.Store{Pool: db}).Detail(ctx, f.md, f.w.ID, p.ProjectID, p.ID)
	monitorWant(t, err, monitor.ErrNotFound)
	// A late smaller UUID is below the cursor: keyset traversal is not a commit log.
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var late string
	if tx.QueryRow(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) VALUES($1,$2,'late','https://example.com/',$3) RETURNING id::text`, f.w.ID, p.ProjectID, f.owner.ID).Scan(&late) != nil {
		t.Fatal("late fixture")
	}
	// Both inserts are guarded SQL fixtures for MVCC cursor semantics, not
	// supported business writers. The supported Store intentionally takes a
	// Project UPDATE lock, which conflicts with the first insert's FK KEY SHARE.
	var later string
	if db.QueryRow(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) VALUES($1,$2,'later','https://example.com/',$3) RETURNING id::text`, f.w.ID, p.ProjectID, f.owner.ID).Scan(&later) != nil {
		t.Fatal("later fixture")
	}
	if later <= late {
		t.Fatal("UUID fixture order")
	}
	if tx.Commit(ctx) != nil {
		t.Fatal("late commit")
	}
	page, err := (monitor.Store{Pool: db}).List(ctx, f.od, f.w.ID, p.ProjectID, 100, later)
	if err != nil || len(page.Data) != 0 {
		t.Fatal("cursor commit ordering")
	}
}

func TestMonitorSameTenantSerializationAndParentReferences(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, p := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	q, err := s.Create(ctx, f.od, f.w.ID, p.ProjectID, monitorInput("other monitor"))
	if err != nil {
		t.Fatal(err)
	}
	pause := newWorkspacePause(t)
	first := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "gate" {
				pause.stop(ctx)
			}
		}}
	}}}
	a := startMonitor(ctx, first, f.od, p, true)
	pause.wait(t)
	pid := make(chan int, 1)
	b := startMonitor(ctx, monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.od, q, true)
	waitWorkspaceBlock(t, db, <-pid)
	pause.resume()
	awaitMonitor(t, a, nil)
	awaitMonitor(t, b, nil)
	for _, insertFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(insertFirst), func(t *testing.T) {
			w, err := (project.Store{Pool: db}).Create(ctx, f.od, f.w.ID, projectInput("fk parent"))
			if err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			insert := `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) VALUES($1,$2,'fk','https://example.com/',$3)`
			del := `DELETE FROM projects WHERE id=$1`
			firstSQL, firstArgs := del, []any{w.ID}
			secondSQL, secondArgs := insert, []any{f.w.ID, w.ID, f.owner.ID}
			if insertFirst {
				firstSQL, firstArgs, secondSQL, secondArgs = insert, []any{f.w.ID, w.ID, f.owner.ID}, del, []any{w.ID}
			}
			if _, err = tx.Exec(ctx, firstSQL, firstArgs...); err != nil {
				t.Fatal(err)
			}
			conn, err := db.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Release()
			var pid int
			conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
			done := make(chan error, 1)
			go func() { _, err := conn.Exec(ctx, secondSQL, secondArgs...); done <- err }()
			waitWorkspaceBlock(t, db, pid)
			if tx.Commit(ctx) != nil {
				t.Fatal("commit")
			}
			if err = <-done; err == nil {
				t.Fatal("FK accepted orphan/delete")
			}
		})
	}
}

func TestMonitorDelayedOwnerRequestCannotUseRejoin(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		t.Run(fmt.Sprint(update), func(t *testing.T) {
			f, p := monitorFixture(t, db)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
				t.Fatal(err)
			}
			pause := newWorkspacePause(t)
			s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
					if stage == "session" {
						pause.stop(ctx)
					}
				}}
			}}}
			ch := startMonitor(ctx, s, f.od, p, update)
			pause.wait(t)
			ws := workspace.Store{Pool: db}
			if err := ws.DeleteMember(ctx, f.od, f.w.ID, f.owner.ID, f.condition(f.w.ID, f.owner.ID)); err != nil {
				t.Fatal(err)
			}
			i, err := ws.CreateInvitation(ctx, f.md, f.w.ID, f.owner.ID)
			if err != nil {
				t.Fatal(err)
			}
			accepted, err := ws.ActOnInvitation(ctx, f.od, i.ID, "accept")
			if err != nil || accepted.Role != "member" {
				t.Fatal("rejoin", err)
			}
			pause.resume()
			awaitMonitor(t, ch, monitor.ErrForbidden)
		})
	}
}

func ptrMonitorString(s string) *string { return &s }

func TestMonitorProjectWriteOrderings(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		for _, projectFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("update%v-projectfirst%v", update, projectFirst), func(t *testing.T) {
				f, m := monitorFixture(t, db)
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				parent, e := (project.Store{Pool: db}).Detail(ctx, f.od, f.w.ID, m.ProjectID)
				if e != nil {
					t.Fatal(e)
				}
				pause := newWorkspacePause(t)
				ppid, mpid := make(chan int, 1), make(chan int, 1)
				ps := project.Store{Pool: workspaceObservedDB{Pool: db, pid: ppid, wrap: func(tx pgx.Tx) pgx.Tx {
					return projectObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						if projectFirst && stage == "gate" {
							pause.stop(ctx)
						}
					}}
				}}}
				ms := monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: mpid, wrap: func(tx pgx.Tx) pgx.Tx {
					return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						if !projectFirst && stage == "gate" {
							pause.stop(ctx)
						}
					}}
				}}}
				var pc <-chan projectResult
				var mc <-chan monitorResult
				if projectFirst {
					pc = startProject(ctx, ps, f.od, parent, true)
					<-ppid
					pause.wait(t)
					mc = startMonitor(ctx, ms, f.od, m, update)
					waitWorkspaceBlock(t, db, <-mpid)
				} else {
					mc = startMonitor(ctx, ms, f.od, m, update)
					<-mpid
					pause.wait(t)
					pc = startProject(ctx, ps, f.od, parent, true)
					waitWorkspaceBlock(t, db, <-ppid)
				}
				pause.resume()
				got := awaitMonitor(t, mc, nil)
				p := awaitProject(t, pc, nil)
				if p.Version != "2" || update && got.Version != "2" || !update && got.Version != "1" {
					t.Fatal("independent versions")
				}
				old, e := (monitor.Store{Pool: db}).Detail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
				if e != nil || !update && monitor.ETag(old) != monitor.ETag(m) {
					t.Fatal("parent edit changed monitor")
				}
			})
		}
	}
}
func TestMonitorCleanupCandidateOrdering(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	ctx := context.Background()
	pause := newWorkspacePause(t)
	s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "candidate" {
				pause.stop(ctx)
			}
		}}
	}}}
	done := startMonitor(ctx, s, f.od, m, false)
	pause.wait(t)
	if _, e := db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.od[:]); e != nil {
		t.Fatal(e)
	}
	if _, e := (session.Store{Pool: db}).Cleanup(ctx); e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, f.od[:]) != 0 {
		t.Fatal("cleanup fixture")
	}
	pause.resume()
	awaitMonitor(t, done, session.ErrInvalid)
}
