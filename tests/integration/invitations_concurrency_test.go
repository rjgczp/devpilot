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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func invitationStage(sql string) string {
	switch {
	case strings.HasPrefix(sql, "SELECT id::text FROM workspace_invitations"):
		return "invitations"
	case strings.HasPrefix(sql, "SELECT user_id::text FROM workspace_members"):
		return "members"
	case strings.HasPrefix(sql, "INSERT INTO workspace_invitations"):
		return "create"
	case strings.HasPrefix(sql, "INSERT INTO workspace_members"):
		return "join"
	case strings.HasPrefix(sql, "UPDATE workspace_invitations"):
		return "transition"
	}
	return memberStage(sql)
}

type invitationObservedTx struct {
	pgx.Tx
	after               func(context.Context, string)
	faultStage, outcome string
	commits             *atomic.Int32
}

func (tx invitationObservedTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	stage := invitationStage(sql)
	if stage == tx.faultStage {
		return workspaceErrorRow{io.ErrUnexpectedEOF}
	}
	return workspaceObservedRow{Row: tx.Tx.QueryRow(ctx, sql, args...), after: func() {
		if tx.after != nil {
			tx.after(ctx, stage)
		}
	}}
}
func (tx invitationObservedTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	stage := invitationStage(sql)
	if stage == tx.faultStage {
		return nil, io.ErrUnexpectedEOF
	}
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return &memberObservedRows{Rows: rows, done: func() {
		if tx.after != nil {
			tx.after(ctx, stage)
		}
	}}, nil
}
func (tx invitationObservedTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	stage := invitationStage(sql)
	if stage == tx.faultStage {
		return pgconn.CommandTag{}, io.ErrUnexpectedEOF
	}
	tag, err := tx.Tx.Exec(ctx, sql, args...)
	if err == nil && tx.after != nil {
		tx.after(ctx, stage)
	}
	return tag, err
}
func (tx invitationObservedTx) Commit(ctx context.Context) error {
	return (workspaceObservedTx{Tx: tx.Tx, commitOutcome: tx.outcome, commits: tx.commits, after: tx.after}).Commit(ctx)
}
func invitationObserved(db *pgxpool.Pool, after func(context.Context, string)) workspace.Store {
	return workspace.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx { return invitationObservedTx{Tx: tx, after: after} }}}
}
func startInvitation(ctx context.Context, s workspace.Store, d session.Digest, id, action string) <-chan error {
	c := make(chan error, 1)
	go func() { _, err := s.ActOnInvitation(ctx, d, id, action); c <- err }()
	return c
}
func TestInvitationActionRaces(t *testing.T) {
	db := workspaceDB(t)
	for _, other := range []string{"accept", "decline", "revoke"} {
		for _, reverse := range []bool{false, true} {
			for _, outcome := range []string{"", "rollback", "lost"} {
				t.Run(fmt.Sprintf("%s-reverse-%v-%s", other, reverse, outcome), func(t *testing.T) {
					f := invitationFixture(t, db)
					i := createInvitation(t, db, f)
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					pause := newWorkspacePause(t)
					a, b, ad, bd := "accept", other, f.md, f.md
					if other == "revoke" {
						bd = f.od
					}
					if reverse {
						a, b = b, a
						ad, bd = bd, ad
					}
					var commits atomic.Int32
					firstStore := workspace.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
						return invitationObservedTx{Tx: tx, outcome: outcome, commits: &commits, after: func(ctx context.Context, stage string) {
							if stage == "invitations" {
								pause.stop(ctx)
							}
						}}
					}}}
					first := startInvitation(ctx, firstStore, ad, i.ID, a)
					pause.wait(t)
					pid := make(chan int, 1)
					second := startInvitation(ctx, workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, bd, i.ID, b)
					waitWorkspaceBlock(t, db, <-pid)
					pause.resume()
					var firstWant error
					secondWant := error(workspace.ErrInvitationNotPending)
					if outcome != "" {
						firstWant = user.ErrUnavailable
					}
					if outcome == "rollback" {
						secondWant = nil
					}
					awaitMember(t, first, firstWant)
					awaitMember(t, second, secondWant)
					if commits.Load() != 1 {
						t.Fatal("commit replay")
					}
					winner := a
					if outcome == "rollback" {
						winner = b
					}
					want := 0
					if winner == "accept" {
						want = 1
					}
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != want {
						t.Fatal("race membership outcome")
					}
				})
			}
		}
	}
}
func fillPending(t *testing.T, db *pgxpool.Pool, f memberFixture, n int) {
	t.Helper()
	_, err := db.Exec(context.Background(), `WITH targets AS(INSERT INTO users(email,password_hash) SELECT uuidv7()::text||'@example.com','fixture-hash' FROM generate_series(1,$3) RETURNING id)
 INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id) SELECT $1,id,$2 FROM targets`, f.w.ID, f.owner.ID, n)
	if err != nil {
		t.Fatal(err)
	}
}
func TestInvitationQuotaAndLazyExpiry(t *testing.T) {
	db := workspaceDB(t)
	s := workspace.Store{Pool: db}
	ctx := context.Background()
	for _, kind := range []string{"duplicate", "quota", "expired-quota", "expired-replace"} {
		t.Run(kind, func(t *testing.T) {
			f := invitationFixture(t, db)
			addFixtureOwner(t, db, f)
			if strings.HasPrefix(kind, "expired") {
				i := createInvitation(t, db, f)
				expireInvitation(t, db, i.ID)
				if kind == "expired-quota" {
					fillPending(t, db, f, 100)
				}
				j, err := s.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
				if kind == "expired-quota" {
					requireInvitationError(t, err, workspace.ErrInvitationLimit)
				} else if err != nil || j.ID == i.ID {
					t.Fatal("expired replacement", err)
				}
				if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='expired' AND updated_at=expires_at`, i.ID) != 1 {
					t.Fatal("business conflict rolled back lazy expiry")
				}
				return
			}
			if kind == "quota" {
				fillPending(t, db, f, 99)
			}
			pause := newWorkspacePause(t)
			store := invitationObserved(db, func(ctx context.Context, stage string) {
				if stage == "invitations" {
					pause.stop(ctx)
				}
			})
			first := make(chan error, 1)
			go func() { _, err := store.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID); first <- err }()
			pause.wait(t)
			target := f.member.ID
			if kind == "quota" {
				u, _, _ := workspaceIdentity(t, db, "quota-second-target@example.com")
				target = u.ID
			}
			pid := make(chan int, 1)
			second := make(chan error, 1)
			go func() {
				_, err := (workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}).CreateInvitation(ctx, f.xd, f.w.ID, target)
				second <- err
			}()
			waitWorkspaceBlock(t, db, <-pid)
			pause.resume()
			awaitMember(t, first, nil)
			want := error(workspace.ErrInvitationPending)
			count := 1
			if kind == "quota" {
				want = workspace.ErrInvitationLimit
				count = 100
			}
			awaitMember(t, second, want)
			if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE workspace_id=$1 AND status='pending' AND expires_at>statement_timestamp()`, f.w.ID) != count {
				t.Fatal("quota oversubscribed")
			}
		})
	}
}
func TestInvitationFaultAtomicity(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, action := range []string{"create", "accept", "decline", "revoke", "expired-accept", "expired-quota"} {
		for _, stage := range []string{"candidate", "user", "session", "workspace", "members", "invitations", "gate", "transition", "join", "create", "rollback", "lost"} {
			if action == "create" && (stage == "join" || stage == "transition") {
				continue
			}
			if action != "create" && stage == "create" {
				continue
			}
			if action != "accept" && stage == "join" {
				continue
			}
			t.Run(action+"-"+stage, func(t *testing.T) {
				f := invitationFixture(t, db)
				var i workspace.OwnerInvitation
				if action != "create" {
					i = createInvitation(t, db, f)
				}
				actualAction := action
				if strings.HasPrefix(action, "expired-") {
					expireInvitation(t, db, i.ID)
					actualAction = "accept"
					if action == "expired-quota" {
						actualAction = "create"
						fillPending(t, db, f, 100)
					}
				}
				var commits atomic.Int32
				outcome, fault := "", stage
				if stage == "lost" || stage == "rollback" {
					outcome = stage
					fault = ""
				}
				store := workspace.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
					return invitationObservedTx{Tx: tx, outcome: outcome, faultStage: fault, commits: &commits}
				}}}
				var err error
				if actualAction == "create" {
					_, err = store.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
				} else {
					d := f.md
					if actualAction == "revoke" {
						d = f.od
					}
					_, err = store.ActOnInvitation(ctx, d, i.ID, actualAction)
				}
				// Stages not used by this operation do not inject an error.
				used := stage == "lost" || stage == "rollback" || stage == "candidate" || stage == "user" || stage == "session" || stage == "workspace" || stage == "members" || stage == "invitations" || stage == "gate" || (stage == "transition" && (actualAction != "create" || strings.HasPrefix(action, "expired"))) || (stage == "join" && action == "accept") || (stage == "create" && action == "create")
				if used {
					requireInvitationError(t, err, user.ErrUnavailable)
				}
				if commits.Load() > 1 {
					t.Fatal("automatic replay")
				}
				if actualAction == "accept" && !strings.HasPrefix(action, "expired") {
					count := queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID)
					accepted := queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='accepted'`, i.ID)
					if count != accepted {
						t.Fatal("partial accepted/member")
					}
					if used && stage != "lost" && count != 0 {
						t.Fatal("rollback retained member")
					}
				}
				if strings.HasPrefix(action, "expired") && used {
					want := 0
					if stage == "lost" {
						want = 1
					}
					if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='expired'`, i.ID) != want {
						t.Fatal("lazy expiry unknown commit")
					}
				}
			})
		}
	}
}
func TestInvitationLockOrderAndIdentity(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, action := range []string{"create", "accept", "decline", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := invitationFixture(t, db)
			var i workspace.OwnerInvitation
			if action != "create" {
				i = createInvitation(t, db, f)
			}
			var stages []string
			s := invitationObserved(db, func(_ context.Context, stage string) { stages = append(stages, stage) })
			var err error
			if action == "create" {
				_, err = s.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
			} else {
				d := f.md
				if action == "revoke" {
					d = f.od
				}
				_, err = s.ActOnInvitation(ctx, d, i.ID, action)
			}
			if err != nil {
				t.Fatal(err)
			}
			sequence := strings.Join(stages, ",")
			expected := "user,session,workspace,members,invitations,gate"
			if action == "create" {
				expected = "user,user,session,workspace,members,invitations,gate"
			}
			if !strings.Contains(sequence, expected) {
				t.Fatal("lock order", sequence)
			}
		})
	}
	for _, kind := range []string{"revoke", "disable", "expire"} {
		for _, action := range []string{"create", "accept", "decline", "revoke"} {
			t.Run(kind+"-"+action, func(t *testing.T) {
				f := invitationFixture(t, db)
				i := createInvitation(t, db, f)
				d, uid := f.md, f.member.ID
				if action == "create" || action == "revoke" {
					d, uid = f.od, f.owner.ID
				}
				sql := `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`
				arg := any(d[:])
				if kind == "disable" {
					sql = `UPDATE users SET status='disabled' WHERE id=$1`
					arg = uid
				}
				if kind == "expire" {
					sql = `UPDATE sessions SET created_at=statement_timestamp()-interval '2h',expires_at=statement_timestamp()-interval '1h' WHERE token_hash=$1`
				}
				if _, err := db.Exec(ctx, sql, arg); err != nil {
					t.Fatal(err)
				}
				s := workspace.Store{Pool: db}
				var err error
				if action == "create" {
					_, err = s.CreateInvitation(ctx, d, f.w.ID, f.member.ID)
				} else {
					_, err = s.ActOnInvitation(ctx, d, i.ID, action)
				}
				requireInvitationError(t, err, session.ErrInvalid)
			})
		}
	}
}

func TestInvitationExpiryAfterEveryLockWait(t *testing.T) {
	db := workspaceDB(t)
	for _, expiry := range []string{"session", "invitation"} {
		for _, action := range []string{"create", "accept", "decline", "revoke"} {
			for _, lock := range []string{"user", "session", "workspace", "members", "invitations"} {
				t.Run(expiry+"-"+action+"-"+lock, func(t *testing.T) {
					f := invitationFixture(t, db)
					i := createInvitation(t, db, f)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					d, uid := f.md, f.member.ID
					if action == "create" || action == "revoke" {
						d, uid = f.od, f.owner.ID
					}
					// A missing target relation is protected by Workspace. To exercise a real
					// Membership row wait for recipient operations, seed an existing relation;
					// identity/expiry must still take precedence over the already-member conflict.
					if lock == "members" && uid == f.member.ID {
						if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.w.ID, f.member.ID); err != nil {
							t.Fatal(err)
						}
					}
					if expiry == "session" {
						expireMemberSoon(t, db, d)
					} else {
						if _, err := db.Exec(ctx, `UPDATE workspace_invitations SET created_at=statement_timestamp()-interval '1d',updated_at=statement_timestamp()-interval '1d',expires_at=statement_timestamp()+interval '300ms' WHERE id=$1`, i.ID); err != nil {
							t.Fatal(err)
						}
					}
					blocker, err := db.Begin(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer blocker.Rollback(context.Background())
					sql, args := `SELECT id FROM users WHERE id=$1 FOR UPDATE`, []any{uid}
					switch lock {
					case "session":
						sql = `SELECT id FROM sessions WHERE token_hash=$1 FOR UPDATE`
						args = []any{d[:]}
					case "workspace":
						sql = `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`
						args = []any{f.w.ID}
					case "members":
						sql = `SELECT user_id FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`
						args = []any{f.w.ID, uid}
					case "invitations":
						sql = `SELECT id FROM workspace_invitations WHERE id=$1 FOR UPDATE`
						args = []any{i.ID}
					}
					if _, err = blocker.Exec(ctx, sql, args...); err != nil {
						t.Fatal(err)
					}
					pid := make(chan int, 1)
					s := workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}
					done := make(chan error, 1)
					go func() {
						var err error
						if action == "create" {
							_, err = s.CreateInvitation(ctx, d, f.w.ID, f.member.ID)
						} else {
							_, err = s.ActOnInvitation(ctx, d, i.ID, action)
						}
						done <- err
					}()
					waitWorkspaceBlock(t, db, <-pid)
					for {
						var expired bool
						sql := `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_hash=$1`
						arg := any(d[:])
						if expiry == "invitation" {
							sql = `SELECT expires_at<=clock_timestamp() FROM workspace_invitations WHERE id=$1`
							arg = i.ID
						}
						if err = blocker.QueryRow(ctx, sql, arg).Scan(&expired); err != nil {
							t.Fatal(err)
						}
						if expired {
							break
						}
						select {
						case <-ctx.Done():
							t.Fatal("expiry clock budget")
						case <-time.After(time.Millisecond):
						}
					}
					if err = blocker.Commit(ctx); err != nil {
						t.Fatal(err)
					}
					want := error(session.ErrInvalid)
					if expiry == "invitation" {
						want = workspace.ErrInvitationExpired
						if action == "create" {
							want = nil
						}
					}
					awaitMember(t, done, want)
				})
			}
		}
	}
}
func TestInvitationPostGateExpiry(t *testing.T) {
	db := workspaceDB(t)
	for _, action := range []string{"create", "accept", "decline", "revoke"} {
		t.Run(action, func(t *testing.T) {
			f := invitationFixture(t, db)
			var i workspace.OwnerInvitation
			if action != "create" {
				i = createInvitation(t, db, f)
				if _, err := db.Exec(context.Background(), `UPDATE workspace_invitations SET created_at=statement_timestamp()-interval '1d',updated_at=statement_timestamp()-interval '1d',expires_at=statement_timestamp()+interval '300ms' WHERE id=$1`, i.ID); err != nil {
					t.Fatal(err)
				}
			}
			d := f.md
			if action == "create" || action == "revoke" {
				d = f.od
			}
			expireMemberSoon(t, db, d)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			p := newWorkspacePause(t)
			s := invitationObserved(db, func(ctx context.Context, stage string) {
				if stage == "gate" {
					p.stop(ctx)
				}
			})
			done := make(chan error, 1)
			go func() {
				var err error
				if action == "create" {
					_, err = s.CreateInvitation(ctx, d, f.w.ID, f.member.ID)
				} else {
					_, err = s.ActOnInvitation(ctx, d, i.ID, action)
				}
				done <- err
			}()
			p.wait(t)
			waitMemberExpiry(t, db, d)
			p.resume()
			awaitMember(t, done, nil)
		})
	}
}
func TestInvitationProtectedIdentityOrderings(t *testing.T) {
	db := workspaceDB(t)
	for _, action := range []string{"create", "accept", "decline", "revoke"} {
		for _, kind := range []string{"revoke", "disable", "role", "remove"} {
			if (kind == "role" || kind == "remove") && (action == "accept" || action == "decline") {
				continue
			}
			for _, changeFirst := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s-%s-change-first-%v", action, kind, changeFirst), func(t *testing.T) {
					f := invitationFixture(t, db)
					var i workspace.OwnerInvitation
					if action != "create" {
						i = createInvitation(t, db, f)
					}
					if kind == "role" || kind == "remove" {
						addFixtureOwner(t, db, f)
					}
					d, uid := f.md, f.member.ID
					if action == "create" || action == "revoke" {
						d, uid = f.od, f.owner.ID
					}
					ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
					defer cancel()
					mutate := func(tx pgx.Tx) error {
						var err error
						switch kind {
						case "revoke":
							_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, d[:])
						case "disable":
							_, err = tx.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, uid)
							if err == nil {
								_, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`, uid)
							}
						case "role", "remove":
							_, err = tx.Exec(ctx, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, f.w.ID)
							if err == nil {
								if kind == "remove" {
									_, err = tx.Exec(ctx, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, uid)
								} else {
									_, err = tx.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, uid)
								}
							}
						}
						return err
					}
					run := func(s workspace.Store) error {
						if action == "create" {
							_, err := s.CreateInvitation(ctx, d, f.w.ID, f.member.ID)
							return err
						}
						_, err := s.ActOnInvitation(ctx, d, i.ID, action)
						return err
					}
					if changeFirst {
						tx, err := db.Begin(ctx)
						if err != nil {
							t.Fatal(err)
						}
						defer tx.Rollback(ctx)
						if err = mutate(tx); err != nil {
							t.Fatal(err)
						}
						pid := make(chan int, 1)
						done := make(chan error, 1)
						go func() { done <- run(workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}) }()
						waitWorkspaceBlock(t, db, <-pid)
						if err = tx.Commit(ctx); err != nil {
							t.Fatal(err)
						}
						want := error(session.ErrInvalid)
						if kind == "role" || kind == "remove" {
							want = workspace.ErrInvitationNotFound
							if action == "create" {
								want = workspace.ErrInvitationForbidden
								if kind == "remove" {
									want = workspace.ErrNotFound
								}
							}
						}
						awaitMember(t, done, want)
					} else {
						p := newWorkspacePause(t)
						s := invitationObserved(db, func(ctx context.Context, stage string) {
							if stage == "invitations" {
								p.stop(ctx)
							}
						})
						done := make(chan error, 1)
						go func() { done <- run(s) }()
						p.wait(t)
						tx, err := db.Begin(ctx)
						if err != nil {
							t.Fatal(err)
						}
						defer tx.Rollback(ctx)
						pid := int(tx.Conn().PgConn().PID())
						change := make(chan error, 1)
						go func() {
							if err := mutate(tx); err != nil {
								change <- err
								return
							}
							change <- tx.Commit(ctx)
						}()
						waitWorkspaceBlock(t, db, pid)
						p.resume()
						awaitMember(t, done, nil)
						awaitMember(t, change, nil)
					}
				})
			}
		}
	}
}

type invitationUserOrderTx struct {
	pgx.Tx
	ids *[]string
}

func (tx invitationUserOrderTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "SELECT status FROM users") {
		*tx.ids = append(*tx.ids, args[0].(string))
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}
func TestInvitationCreationUserUUIDOrder(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	var ids []string
	s := workspace.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx { return invitationUserOrderTx{Tx: tx, ids: &ids} }}}
	if _, err := s.CreateInvitation(context.Background(), f.xd, f.z.ID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != f.owner.ID || ids[1] != f.other.ID || ids[0] >= ids[1] {
		t.Fatal("user lock UUID order", ids)
	}
}

func TestInvitationSessionCleanupOrderings(t *testing.T) {
	db := workspaceDB(t)
	for _, action := range []string{"create", "accept", "decline", "revoke"} {
		for _, first := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-cleanup-first-%v", action, first), func(t *testing.T) {
				f := invitationFixture(t, db)
				var i workspace.OwnerInvitation
				if action != "create" {
					i = createInvitation(t, db, f)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				d := f.md
				if action == "create" || action == "revoke" {
					d = f.od
				}
				run := func(s workspace.Store) error {
					if action == "create" {
						_, err := s.CreateInvitation(ctx, d, f.w.ID, f.member.ID)
						return err
					}
					_, err := s.ActOnInvitation(ctx, d, i.ID, action)
					return err
				}
				cleanup := session.Store{Pool: db}
				if first {
					if _, err := db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, d[:]); err != nil {
						t.Fatal(err)
					}
					if _, err := cleanup.Cleanup(ctx); err != nil {
						t.Fatal(err)
					}
					requireInvitationError(t, run(workspace.Store{Pool: db}), session.ErrInvalid)
				} else {
					expireMemberSoon(t, db, d)
					p := newWorkspacePause(t)
					s := invitationObserved(db, func(ctx context.Context, stage string) {
						if stage == "gate" {
							p.stop(ctx)
						}
					})
					done := make(chan error, 1)
					go func() { done <- run(s) }()
					p.wait(t)
					waitMemberExpiry(t, db, d)
					if _, err := cleanup.Cleanup(ctx); err != nil {
						t.Fatal(err)
					}
					if queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, d[:]) != 1 {
						t.Fatal("cleanup bypassed protective Session lock")
					}
					p.resume()
					awaitMember(t, done, nil)
					if _, err := cleanup.Cleanup(ctx); err != nil {
						t.Fatal(err)
					}
					requireInvitationError(t, run(workspace.Store{Pool: db}), session.ErrInvalid)
				}
			})
		}
	}
}
