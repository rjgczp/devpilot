//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/workspace"
)

// Initially run against the unmodified production DeleteMember protocol. The
// red evidence predates the conditional-delete implementation, not a SQL mock.
func TestDelayedDeletePreservesRejoin(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	p := newWorkspacePause(t)
	observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return memberObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "session" {
				p.stop(ctx)
			}
		}}
	}}
	done := startMemberDelete(ctx, workspace.Store{Pool: observed}, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID))
	p.wait(t)
	if err := (workspace.Store{Pool: db}).DeleteMember(ctx, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil {
		t.Fatal(err)
	}
	s := workspace.Store{Pool: db}
	i, err := s.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.ActOnInvitation(ctx, f.md, i.ID, "accept")
	if err != nil {
		t.Fatal(err)
	}
	p.resume()
	awaitMember(t, done, workspace.ErrPreconditionFailed)
	if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND membership_id=$3`, f.w.ID, f.member.ID, a.MembershipID) != 1 {
		t.Fatal("OLD DELETE REMOVED THE REJOINED RELATIONSHIP")
	}
}

func TestInvitationDeleteHeldLockAndSelfReplay(t *testing.T) {
	db := workspaceDB(t)
	for _, actor := range []string{"owner", "self"} {
		for _, held := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-workspace-held-%v", actor, held), func(t *testing.T) {
				f := invitationFixture(t, db)
				i := createInvitation(t, db, f)
				ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer cancel()
				s := workspace.Store{Pool: db}
				m1, err := s.ActOnInvitation(ctx, f.md, i.ID, "accept")
				if err != nil {
					t.Fatal(err)
				}
				condition := workspace.MemberCondition(m1.MembershipID, "member")
				d := f.od
				if actor == "self" {
					d = f.md
				}
				p := newWorkspacePause(t)
				stage := "session"
				if held {
					stage = "workspace"
				}
				observed := workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
					return memberObservedTx{Tx: tx, after: func(ctx context.Context, got string) {
						if got == stage {
							p.stop(ctx)
						}
					}}
				}}
				done := startMemberDelete(ctx, workspace.Store{Pool: observed}, d, f.w.ID, f.member.ID, condition)
				p.wait(t)
				if !held {
					if err = s.DeleteMember(ctx, f.od, f.w.ID, f.member.ID, condition); err != nil {
						t.Fatal(err)
					}
					j, err := s.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
					if err != nil {
						t.Fatal(err)
					}
					m2, err := s.ActOnInvitation(ctx, f.md, j.ID, "accept")
					if err != nil {
						t.Fatal(err)
					}
					p.resume()
					awaitMember(t, done, workspace.ErrPreconditionFailed)
					if m1.MembershipID == m2.MembershipID || queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE membership_id=$1`, m2.MembershipID) != 1 {
						t.Fatal("M2 not preserved")
					}
				} else {
					pid := make(chan int, 1)
					next := make(chan workspace.OwnerInvitation, 1)
					errs := make(chan error, 1)
					go func() {
						j, err := (workspace.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}).CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
						next <- j
						errs <- err
					}()
					waitWorkspaceBlock(t, db, <-pid)
					p.resume()
					awaitMember(t, done, nil)
					awaitMember(t, errs, nil)
					j := <-next
					m2, err := s.ActOnInvitation(ctx, f.md, j.ID, "accept")
					if err != nil {
						t.Fatal(err)
					}
					requireInvitationError(t, s.DeleteMember(ctx, d, f.w.ID, f.member.ID, condition), workspace.ErrPreconditionFailed)
					if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE membership_id=$1`, m2.MembershipID) != 1 {
						t.Fatal("old held request removed M2")
					}
				}
			})
		}
	}
}
