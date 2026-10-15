//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

type memberFixture struct {
	validators                          map[string]string
	owner, member, other                user.Public
	ownerToken, memberToken, otherToken string
	od, md, xd                          session.Digest
	w, z                                workspace.Public
}

func membersFixture(t *testing.T, db *pgxpool.Pool) memberFixture {
	t.Helper()
	var f memberFixture
	ctx := context.Background()
	suffix := strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	f.owner, f.ownerToken, f.od = workspaceIdentity(t, db, suffix+"-o@example.com")
	f.member, f.memberToken, f.md = workspaceIdentity(t, db, suffix+"-m@example.com")
	f.other, f.otherToken, f.xd = workspaceIdentity(t, db, suffix+"-x@example.com")
	s := workspace.Store{Pool: db}
	var err error
	f.w, err = s.Create(ctx, f.od, "member tenant")
	if err != nil {
		t.Fatal("member fixture workspace")
	}
	f.z, err = s.Create(ctx, f.xd, "other tenant")
	if err != nil {
		t.Fatal("other fixture workspace")
	}
	if _, err = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member'),($3,$2,'member')`, f.w.ID, f.member.ID, f.z.ID); err != nil {
		t.Fatal("member fixture relations")
	}
	f.validators = make(map[string]string)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name='workspace_members' AND column_name='membership_id'`) > 0 {
		rows, e := db.Query(ctx, `SELECT workspace_id::text,user_id::text,membership_id::text,role FROM workspace_members WHERE workspace_id=$1 OR workspace_id=$2`, f.w.ID, f.z.ID)
		if e != nil {
			t.Fatal(e)
		}
		for rows.Next() {
			var w, u, id, role string
			if e = rows.Scan(&w, &u, &id, &role); e != nil {
				t.Fatal(e)
			}
			f.validators[w+u] = workspace.MemberCondition(id, role)
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		rows.Close()
	}
	return f
}

func TestMemberRosterStore(t *testing.T) {
	db := workspaceDB(t)
	f := membersFixture(t, db)
	s := workspace.Store{Pool: db}
	ctx := context.Background()
	for _, d := range []session.Digest{f.od, f.md} {
		p, err := s.ListMembers(ctx, d, f.w.ID, 50, "")
		if err != nil || len(p.Data) != 2 || p.Pagination.NextAfter != nil {
			t.Fatal("authorized roster", err)
		}
		for _, m := range p.Data {
			if m.JoinedAt.IsZero() || m.JoinedAt.Location().String() != "UTC" {
				t.Fatal("joined time")
			}
		}
	}
	for _, id := range []string{f.z.ID, "bad", strings.ToUpper(f.w.ID), "00000000-0000-0000-0000-000000000000"} {
		if _, err := s.ListMembers(ctx, f.od, id, 50, ""); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatal("hidden roster", err)
		}
	}
	if _, err := s.ListMembers(ctx, session.Digest{}, "bad", 50, ""); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("identity precedes resource", err)
	}
	first, err := s.ListMembers(ctx, f.md, f.w.ID, 1, "")
	if err != nil || len(first.Data) != 1 || first.Pagination.NextAfter == nil {
		t.Fatal("first page")
	}
	second, err := s.ListMembers(ctx, f.md, f.w.ID, 1, *first.Pagination.NextAfter)
	if err != nil || len(second.Data) != 1 || second.Data[0].UserID <= first.Data[0].UserID || second.Pagination.NextAfter != nil {
		t.Fatal("next page")
	}
	empty, err := s.ListMembers(ctx, f.md, f.w.ID, 100, "ffffffff-ffff-ffff-ffff-ffffffffffff")
	if err != nil || empty.Data == nil || len(empty.Data) != 0 || empty.Pagination.NextAfter != nil {
		t.Fatal("authorized empty suffix")
	}
	if _, err := s.ListMembers(ctx, f.md, f.w.ID, 100, f.other.ID); err != nil {
		t.Fatal("foreign cursor used as authority")
	}
	if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.member.ID); err != nil {
		t.Fatal("disable fixture")
	}
	p, err := s.ListMembers(ctx, f.od, f.w.ID, 50, "")
	if err != nil || len(p.Data) != 2 {
		t.Fatal("disabled relation should remain")
	}
	if _, err := s.ListMembers(ctx, f.md, f.w.ID, 50, ""); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("disabled caller")
	}
	for _, n := range []int{0, 101} {
		if _, err := s.ListMembers(ctx, f.od, f.w.ID, n, ""); !errors.Is(err, user.ErrInternal) {
			t.Fatal("defensive bounds")
		}
	}
}

func TestMemberDeleteStore(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	s := workspace.Store{Pool: db}
	for _, kind := range []string{"owner-member", "member-self", "member-other", "owner-self", "owner-other-owner", "two-owner-self", "missing", "foreign-target", "disabled-target", "malformed-target", "outside", "invalid-identity", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			f := membersFixture(t, db)
			actor, target, wid := f.od, f.member.ID, f.w.ID
			var want error
			switch kind {
			case "member-self":
				actor = f.md
			case "member-other":
				actor = f.md
				target = f.other.ID
				want = workspace.ErrMemberForbidden
			case "owner-self":
				target = f.owner.ID
				want = workspace.ErrLastOwner
			case "owner-other-owner", "two-owner-self":
				if _, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.w.ID, f.other.ID); err != nil {
					t.Fatal("multi owner fixture")
				}
				if kind == "owner-other-owner" {
					target = f.other.ID
					want = workspace.ErrMemberForbidden
				} else {
					target = f.owner.ID
				}
			case "missing":
				want = workspace.ErrPreconditionFailed
				target = "00000000-0000-0000-0000-000000000000"
			case "foreign-target":
				want = workspace.ErrPreconditionFailed
				target = f.other.ID
			case "disabled-target":
				if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.member.ID); err != nil {
					t.Fatal("disable")
				}
			case "malformed-target":
				target = "bad"
				want = workspace.ErrMemberNotFound
			case "outside":
				actor = f.xd
				want = workspace.ErrNotFound
			case "invalid-identity":
				actor = session.Digest{}
				wid = "bad"
				target = "bad"
				want = session.ErrInvalid
			case "corrupt":
				actor = f.md
				target = f.member.ID
				want = user.ErrInternal
				if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1`, f.w.ID); err != nil {
					t.Fatal("guarded corruption")
				}
			}
			err := s.DeleteMember(ctx, actor, wid, target, f.condition(wid, target))
			if !errors.Is(err, want) {
				t.Fatalf("delete got %v want %v", err, want)
			}
			if want == nil {
				if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, target) != 0 {
					t.Fatal("target relation retained")
				}
				retry := s.DeleteMember(ctx, actor, wid, target, f.condition(wid, target))
				if kind == "member-self" || kind == "two-owner-self" {
					if !errors.Is(retry, workspace.ErrNotFound) {
						t.Fatal("stale self permission", retry)
					}
				} else if !errors.Is(retry, workspace.ErrPreconditionFailed) {
					t.Fatal("owner repeat", retry)
				}
			}
			if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.z.ID, f.member.ID) != 1 {
				t.Fatal("cross tenant deletion")
			}
			if kind != "corrupt" && queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND role='owner'`, f.w.ID) < 1 {
				t.Fatal("lost final owner")
			}
		})
	}
}

// Captured at fixture creation, never re-read or refreshed by a retry helper.
func (f memberFixture) condition(wid, target string) string {
	if c, ok := f.validators[wid+target]; ok {
		return c
	}
	return workspace.MemberCondition("00000000-0000-0000-0000-000000000000", "member")
}
