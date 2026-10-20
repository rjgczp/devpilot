//go:build integration

package integration

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/workspace"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type activationObservedTx struct {
	pgx.Tx
	after   func(context.Context, string)
	outcome string
	commits *atomic.Int32
}

func (tx activationObservedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	stage := monitorStage(sql)
	if strings.HasPrefix(sql, "SELECT id::text FROM http_monitors") {
		stage = "monitor"
	}
	if strings.HasPrefix(sql, "SELECT monitor_id::text FROM monitor_activation_states") {
		stage = "activation"
	}
	return workspaceObservedRow{Row: tx.Tx.QueryRow(ctx, sql, args...), after: func() {
		if tx.after != nil {
			tx.after(ctx, stage)
		}
	}}
}
func (tx activationObservedTx) Commit(ctx context.Context) error {
	return (workspaceObservedTx{Tx: tx.Tx, commitOutcome: tx.outcome, commits: tx.commits}).Commit(ctx)
}

type activationResult struct {
	a   monitor.Activation
	err error
}

func startActivation(ctx context.Context, s monitor.Store, d session.Digest, m monitor.Public, pause bool) <-chan activationResult {
	ch := make(chan activationResult, 1)
	// Read the condition before launching a writer; no cached authority is granted.
	a, e := (monitor.Store{Pool: s.Pool}).ActivationDetail(ctx, d, m.WorkspaceID, m.ProjectID, m.ID)
	if e != nil {
		ch <- activationResult{err: e}
		return ch
	}
	go func() {
		state := "enabled"
		if pause {
			state = "disabled"
		}
		got, e := s.ActivationUpdate(ctx, d, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: state, ConfigurationVersion: a.ConfigurationVersion}, monitor.ActivationETag(a))
		ch <- activationResult{got, e}
	}()
	return ch
}
func awaitActivation(t *testing.T, ch <-chan activationResult, want error) monitor.Activation {
	t.Helper()
	select {
	case r := <-ch:
		monitorWant(t, r.err, want)
		return r.a
	case <-time.After(5 * time.Second):
		t.Fatal("activation did not finish")
		return monitor.Activation{}
	}
}
func TestActivationExpiryAfterEveryProtectiveWait(t *testing.T) {
	db := workspaceDB(t)
	for _, update := range []bool{false, true} {
		for _, stage := range []string{"user", "session", "workspace", "membership", "project", "monitor", "activation", "post-gate"} {
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
					return activationObservedTx{Tx: tx, after: func(ctx context.Context, s string) {
						if stage == "post-gate" && s == "gate" {
							pause.stop(ctx)
						}
					}}
				}}}
				var ch <-chan activationResult
				if stage == "post-gate" {
					ch = startActivation(ctx, store, f.od, p, update)
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
					awaitActivation(t, ch, nil)
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
					case "activation":
						sql = `SELECT monitor_id FROM monitor_activation_states WHERE monitor_id=$1 FOR UPDATE`
						arg = p.ID
					case "monitor":
						sql = `SELECT id FROM http_monitors WHERE id=$1 FOR UPDATE`
						arg = p.ID
					}
					if _, err = blocker.Exec(ctx, sql, arg); err != nil {
						t.Fatal(err)
					}
					ch = startActivation(ctx, store, f.od, p, update)
					waitActivationBlock(t, db, <-pid)
					waitWorkspaceTime(t, db, expiry)
					if blocker.Commit(ctx) != nil {
						t.Fatal("release")
					}
					awaitActivation(t, ch, session.ErrInvalid)
				}
			})
		}
	}
}
func TestActivationIdentityAndMembershipOrderings(t *testing.T) {
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
						return activationObservedTx{Tx: tx, after: func(ctx context.Context, s string) {
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
					var action <-chan activationResult
					if monitorFirst {
						action = startActivation(ctx, ps, f.od, p, update)
						<-monitorPID
						pause.wait(t)
						go func() { changes <- change() }()
						waitActivationBlock(t, db, <-changePID)
					} else {
						go func() { changes <- change() }()
						<-changePID
						pause.wait(t)
						action = startActivation(ctx, ps, f.od, p, update)
						waitActivationBlock(t, db, <-monitorPID)
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
					awaitActivation(t, action, want)
					awaitMember(t, changes, nil)
				})
			}
		}
	}
}
func TestActivationDelayedOwnerRequestCannotUseRejoin(t *testing.T) {
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
				return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
					if stage == "session" {
						pause.stop(ctx)
					}
				}}
			}}}
			ch := startActivation(ctx, s, f.od, p, update)
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
			awaitActivation(t, ch, monitor.ErrForbidden)
		})
	}
}

func TestActivationCleanupCandidateOrdering(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	ctx := context.Background()
	pause := newWorkspacePause(t)
	s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "candidate" {
				pause.stop(ctx)
			}
		}}
	}}}
	done := startActivation(ctx, s, f.od, m, false)
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
	awaitActivation(t, done, session.ErrInvalid)
}
func TestActivationTwoOwnerCASAndConfigOrderings(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, kind := range []string{"two-owners", "patch-first", "enable-first"} {
		t.Run(kind, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			if _, e := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); e != nil {
				t.Fatal(e)
			}
			pause := newWorkspacePause(t)
			pid := make(chan int, 1)
			observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
				return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
					if stage == "gate" {
						pause.stop(ctx)
					}
				}}
			}}
			if kind == "patch-first" {
				first := startMonitor(ctx, monitor.Store{Pool: observed}, f.od, m, true)
				pause.wait(t)
				second := startActivation(ctx, monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.md, m, false)
				waitActivationBlock(t, db, <-pid)
				pause.resume()
				awaitMonitor(t, first, nil)
				awaitActivation(t, second, monitor.ErrPreconditionFailed)
			} else {
				first := startActivation(ctx, monitor.Store{Pool: observed}, f.od, m, false)
				pause.wait(t)
				if kind == "two-owners" {
					second := startActivation(ctx, monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.md, m, true)
					waitActivationBlock(t, db, <-pid)
					pause.resume()
					awaitActivation(t, first, nil)
					awaitActivation(t, second, monitor.ErrPreconditionFailed)
				} else {
					second := startMonitor(ctx, monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.md, m, true)
					waitActivationBlock(t, db, <-pid)
					pause.resume()
					awaitActivation(t, first, nil)
					awaitMonitor(t, second, nil)
				}
			}
			a := activationRead(t, db, f.od, m)
			if kind != "two-owners" && (a.ConfigurationVersion != "2" || a.DesiredState != "disabled") {
				t.Fatal("old intent survived revision")
			}
		})
	}
}

func TestActivationProjectWriteOrderings(t *testing.T) {
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
					return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						if !projectFirst && stage == "gate" {
							pause.stop(ctx)
						}
					}}
				}}}
				var pc <-chan projectResult
				var mc <-chan activationResult
				if projectFirst {
					pc = startProject(ctx, ps, f.od, parent, true)
					<-ppid
					pause.wait(t)
					mc = startActivation(ctx, ms, f.od, m, update)
					waitActivationBlock(t, db, <-mpid)
				} else {
					mc = startActivation(ctx, ms, f.od, m, update)
					<-mpid
					pause.wait(t)
					pc = startProject(ctx, ps, f.od, parent, true)
					waitActivationBlock(t, db, <-ppid)
				}
				pause.resume()
				got := awaitActivation(t, mc, nil)
				p := awaitProject(t, pc, nil)
				if p.Version != "2" || got.ConfigurationVersion != "1" {
					t.Fatal("independent versions")
				}
				old, e := (monitor.Store{Pool: db}).Detail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
				if e != nil || monitor.ETag(old) != monitor.ETag(m) {
					t.Fatal("parent edit changed monitor")
				}
			})
		}
	}
}

func waitActivationBlock(t *testing.T, db *pgxpool.Pool, pid int) {
	t.Helper()
	waitWorkspaceBlock(t, db, pid)
	if queryInt(t, db, `SELECT count(*) FROM pg_locks WHERE pid=$1 AND NOT granted`, pid) == 0 || queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock'`, pid) != 1 {
		t.Fatal("missing pg_locks/pg_stat_activity wait evidence")
	}
}
