//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func invitationFixture(t *testing.T, db *pgxpool.Pool) memberFixture {
	t.Helper()
	f := membersFixture(t, db)
	if err := (workspace.Store{Pool: db}).DeleteMember(context.Background(), f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil {
		t.Fatal(err)
	}
	return f
}
func createInvitation(t *testing.T, db *pgxpool.Pool, f memberFixture) workspace.OwnerInvitation {
	t.Helper()
	i, err := (workspace.Store{Pool: db}).CreateInvitation(context.Background(), f.od, f.w.ID, f.member.ID)
	if err != nil {
		t.Fatal("create invitation", err)
	}
	return i
}
func expireInvitation(t *testing.T, db *pgxpool.Pool, id string) {
	t.Helper()
	_, err := db.Exec(context.Background(), `UPDATE workspace_invitations SET created_at=statement_timestamp()-interval '8 days',updated_at=statement_timestamp()-interval '8 days',expires_at=statement_timestamp()-interval '1 second' WHERE id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
}
func requireInvitationError(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
func TestInvitationMigration(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 3", true)
	f := membersFixture(t, db)
	before := queryInt(t, db, `SELECT count(*) FROM workspace_members`)
	var joined time.Time
	if err := db.QueryRow(ctx, `SELECT created_at FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	goose(t, dir, "up", true)
	var id string
	var after time.Time
	if err := db.QueryRow(ctx, `SELECT membership_id::text,created_at FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID).Scan(&id, &after); err != nil || !after.Equal(joined) || id[14] != '7' {
		t.Fatal("backfill preservation", err)
	}
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE membership_id=$1`, id) != 1 || queryInt(t, db, `SELECT count(DISTINCT membership_id) FROM workspace_members`) != before {
		t.Fatal("backfill uniqueness/repeat")
	}
	for _, sql := range []string{`UPDATE workspace_members SET membership_id=NULL`, `UPDATE workspace_members SET membership_id='00000000-0000-0000-0000-000000000000'`} {
		if _, err := db.Exec(ctx, sql); err == nil {
			t.Fatal("generation constraint")
		}
	}
	i, err := (workspace.Store{Pool: db}).CreateInvitation(ctx, f.od, f.w.ID, f.other.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE workspace_invitations SET status='owner'`, `UPDATE workspace_invitations SET expires_at=created_at`, `UPDATE workspace_invitations SET updated_at=expires_at+interval '1s'`, `UPDATE workspace_invitations SET invitee_user_id=created_by_user_id`, `UPDATE workspace_invitations SET invitee_user_id=uuidv7()`, `UPDATE workspace_invitations SET workspace_id=uuidv7()`, `INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id) SELECT workspace_id,invitee_user_id,created_by_user_id FROM workspace_invitations`} {
		if _, err := db.Exec(ctx, sql); err == nil {
			t.Fatal("invitation constraint accepted", sql)
		}
	}
	if _, err = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.other.ID); err == nil {
		t.Fatal("referenced invitation user deletion")
	}
	if _, err = db.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, f.w.ID); err != nil || queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1`, i.ID) != 0 {
		t.Fatal("invitation cascade", err)
	}
	left := queryInt(t, db, `SELECT count(*) FROM workspace_members`)
	goose(t, dir, "down", true)
	if queryInt(t, db, `SELECT count(*) FROM workspace_members`) != left || queryInt(t, db, `SELECT count(*) FROM users`) != 3 {
		t.Fatal("down damaged v3 data")
	}
	goose(t, dir, "up", true)
	// Fail the real migration after its schema/backfill work, preserving version3.
	goose(t, dir, "down", true)
	tmp := t.TempDir()
	for _, file := range []string{"00001_create_users.sql", "00002_create_sessions.sql", "00003_create_workspaces.sql", "00004_create_workspace_invitations.sql"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(file, "00004") {
			b = []byte(strings.Replace(string(b), "-- +goose Down", "SELECT 1/0;\n-- +goose Down", 1))
		}
		if err = os.WriteFile(filepath.Join(tmp, file), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	goose(t, tmp, "up", false)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name='workspace_members' AND column_name='membership_id'`) != 0 || queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_name='workspace_invitations'`) != 0 || queryInt(t, db, `SELECT count(*) FROM workspace_members`) != left {
		t.Fatal("failed migration not atomic")
	}
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT count(*) FROM pg_indexes WHERE tablename='workspace_invitations'`) != 8 {
		t.Fatal("invitation indexes")
	}
}
func TestInvitationLifecycle(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	s := workspace.Store{Pool: db}
	for _, kind := range []string{"accept", "decline", "revoke", "expire-accept", "expire-decline", "expire-revoke", "creator-lost", "creator-demoted", "creator-revoked", "ownerless", "existing", "privacy"} {
		t.Run(kind, func(t *testing.T) {
			f := invitationFixture(t, db)
			i := createInvitation(t, db, f)
			if i.Status != "pending" || i.ExpiresAt.Sub(i.CreatedAt) != 168*time.Hour || !i.CreatedAt.Equal(i.UpdatedAt) {
				t.Fatal("initial invitation")
			}
			if queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 0 {
				t.Fatal("invitation joined without consent")
			}
			_, err := s.Detail(ctx, f.md, f.w.ID)
			requireInvitationError(t, err, workspace.ErrNotFound)
			_, err = s.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
			requireInvitationError(t, err, workspace.ErrInvitationPending)
			for _, target := range []string{f.owner.ID, "bad", "00000000-0000-0000-0000-000000000000"} {
				_, err = s.CreateInvitation(ctx, f.od, f.w.ID, target)
				requireInvitationError(t, err, workspace.ErrInviteeUnavailable)
			}
			for _, a := range []string{"accept", "decline", "revoke"} {
				_, err = s.ActOnInvitation(ctx, f.xd, i.ID, a)
				requireInvitationError(t, err, workspace.ErrInvitationNotFound)
			}
			if kind == "privacy" {
				p, err := s.ListInvitations(ctx, f.md, "", false, 1, "", "pending")
				if err != nil || len(p.Data) != 1 || p.Data[0].(workspace.RecipientInvitation).WorkspaceName == nil {
					t.Fatal("inbox preview", err)
				}
				_, err = s.ActOnInvitation(ctx, f.md, i.ID, "decline")
				if err != nil {
					t.Fatal(err)
				}
				db.Exec(ctx, `UPDATE workspaces SET name='NEW SECRET NAME' WHERE id=$1`, f.w.ID)
				p, err = s.ListInvitations(ctx, f.md, "", false, 50, "", "all")
				if err != nil || p.Data[0].(workspace.RecipientInvitation).WorkspaceName != nil {
					t.Fatal("terminal name disclosure")
				}
				return
			}
			if kind == "ownerless" {
				db.Exec(ctx, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1`, f.w.ID)
				_, err = s.ActOnInvitation(ctx, f.md, i.ID, "accept")
				requireInvitationError(t, err, user.ErrInternal)
				return
			}
			if kind == "existing" {
				db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, f.w.ID, f.member.ID)
				_, err = s.ActOnInvitation(ctx, f.md, i.ID, "accept")
				requireInvitationError(t, err, workspace.ErrAlreadyMember)
				return
			}
			action, digest := kind, f.md
			if kind == "revoke" {
				digest = f.od
			}
			if strings.HasPrefix(kind, "expire-") {
				expireInvitation(t, db, i.ID)
				action = strings.TrimPrefix(kind, "expire-")
				if action == "revoke" {
					digest = f.od
				}
				_, err = s.ActOnInvitation(ctx, digest, i.ID, action)
				requireInvitationError(t, err, workspace.ErrInvitationExpired)
				if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='expired' AND updated_at=expires_at`, i.ID) != 1 {
					t.Fatal("lazy expiry rolled back")
				}
				return
			}
			if strings.HasPrefix(kind, "creator-") {
				addFixtureOwner(t, db, f)
				switch kind {
				case "creator-lost":
					mustInvitationExec(t, db, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID)
					mustInvitationExec(t, db, `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
				case "creator-demoted":
					mustInvitationExec(t, db, `UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.owner.ID)
				case "creator-revoked":
					mustInvitationExec(t, db, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.od[:])
				}
				action = "accept"
			}
			a, err := s.ActOnInvitation(ctx, digest, i.ID, action)
			if err != nil {
				t.Fatal("action", err)
			}
			for _, again := range []string{"accept", "decline"} {
				_, err = s.ActOnInvitation(ctx, f.md, i.ID, again)
				requireInvitationError(t, err, workspace.ErrInvitationNotPending)
			}
			if action == "accept" {
				if a.Role != "member" || a.MembershipID == "" {
					t.Fatal("accepted projection")
				}
				_, err = s.Detail(ctx, f.md, f.w.ID)
				if err != nil {
					t.Fatal("immediate permission", err)
				}
				if err = s.DeleteMember(ctx, f.md, f.w.ID, f.member.ID, workspace.MemberCondition(a.MembershipID, "member")); err != nil {
					t.Fatal(err)
				}
				_, err = s.ActOnInvitation(ctx, f.md, i.ID, "accept")
				requireInvitationError(t, err, workspace.ErrInvitationNotPending)
				// New Owner creates the new invitation after creator loss.
				od := f.od
				if strings.HasPrefix(kind, "creator-") {
					od = f.xd
				}
				j, err := s.CreateInvitation(ctx, od, f.w.ID, f.member.ID)
				if err != nil {
					t.Fatal(err)
				}
				b, err := s.ActOnInvitation(ctx, f.md, j.ID, "accept")
				if err != nil || b.MembershipID == a.MembershipID {
					t.Fatal("rejoin generation", err)
				}
				requireInvitationError(t, s.DeleteMember(ctx, od, f.w.ID, f.member.ID, workspace.MemberCondition(a.MembershipID, "member")), workspace.ErrPreconditionFailed)
			}
		})
	}
}
func TestInvitationHTTP(t *testing.T) {
	db := workspaceDB(t)
	f := invitationFixture(t, db)
	var logs bytes.Buffer
	server, _ := authServer(t, db, false, &logs)
	o := workspaceCookieClient(t, server, f.ownerToken, false)
	m := workspaceCookieClient(t, server, f.memberToken, false)
	x := workspaceCookieClient(t, server, f.otherToken, false)
	path := "/api/v1/workspaces/" + f.w.ID + "/invitations"
	body := `{"invitee_user_id":"` + f.member.ID + `"}`
	status, _, b := workspaceRequest(t, o, server.URL, "POST", path, body, true)
	var v struct{ Data workspace.OwnerInvitation }
	if status != 201 || json.Unmarshal(b, &v) != nil {
		t.Fatal("HTTP create", status, string(b))
	}
	for _, item := range []struct {
		client bool
		path   string
	}{{false, path}, {true, "/api/v1/invitations"}} {
		c := o
		if item.client {
			c = m
		}
		status, _, b = workspaceRequest(t, c, server.URL, "GET", item.path, "", false)
		if status != 200 {
			t.Fatal("HTTP list", status, string(b))
		}
	}
	status, _, b = workspaceRequest(t, x, server.URL, "POST", "/api/v1/invitations/"+v.Data.ID+"/accept", "", true)
	if status != 404 {
		t.Fatal("target isolation", status)
	}
	requireCode(t, b, "invitation_not_found")
	status, _, b = workspaceRequest(t, m, server.URL, "POST", "/api/v1/invitations/"+v.Data.ID+"/accept", "", true)
	var a struct{ Data workspace.Acceptance }
	if status != 200 || json.Unmarshal(b, &a) != nil {
		t.Fatal("HTTP accept", status, string(b))
	}
	status, _, _ = workspaceRequest(t, m, server.URL, "GET", path, "", false)
	if status != 403 {
		t.Fatal("member managed invitations", status)
	}
	memberPath := "/api/v1/workspaces/" + f.w.ID + "/members/" + f.member.ID
	status, _, _ = workspaceRequest(t, o, server.URL, "DELETE", memberPath, "", true)
	if status != 428 {
		t.Fatal("missing precondition", status)
	}
	status, _, _ = workspaceRequest(t, o, server.URL, "DELETE", memberPath, "", true, workspace.MemberCondition(a.Data.MembershipID, "member"))
	if status != 204 {
		t.Fatal("conditional removal", status)
	}
	status, _, _ = workspaceRequest(t, o, server.URL, "DELETE", memberPath, "", true, workspace.MemberCondition(a.Data.MembershipID, "member"))
	if status != 412 {
		t.Fatal("stale replay", status)
	}
	for _, action := range []string{"decline", "revoke"} {
		status, _, b = workspaceRequest(t, o, server.URL, "POST", path, body, true)
		json.Unmarshal(b, &v)
		if status != 201 {
			t.Fatal("new invite", status)
		}
		method, url, c := "POST", "/api/v1/invitations/"+v.Data.ID+"/decline", m
		if action == "revoke" {
			method = "DELETE"
			url = "/api/v1/invitations/" + v.Data.ID
			c = o
		}
		status, h, b := workspaceRequest(t, c, server.URL, method, url, "", true)
		if status != 204 || len(b) != 0 || h.Get("Content-Type") != "" {
			t.Fatal("HTTP terminal", action, status)
		}
	}
	if strings.Contains(logs.String(), f.member.ID) || strings.Contains(logs.String(), f.memberToken) {
		t.Fatal("unsafe invitation logs")
	}
}

func TestInvitationListsAndTenantPermissions(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	s := workspace.Store{Pool: db}
	f := invitationFixture(t, db)
	i := createInvitation(t, db, f)
	j, err := s.CreateInvitation(ctx, f.od, f.w.ID, f.other.ID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ListInvitations(ctx, f.od, f.w.ID, true, 1, "", "pending")
	if err != nil || len(p.Data) != 1 || p.Pagination.NextAfter == nil {
		t.Fatal("first owner page", err)
	}
	p2, err := s.ListInvitations(ctx, f.od, f.w.ID, true, 1, *p.Pagination.NextAfter, "pending")
	if err != nil || len(p2.Data) != 1 || p2.Pagination.NextAfter != nil || p.Data[0].(workspace.OwnerInvitation).ID >= p2.Data[0].(workspace.OwnerInvitation).ID {
		t.Fatal("second page", err)
	}
	empty, err := s.ListInvitations(ctx, f.od, f.w.ID, true, 100, "ffffffff-ffff-ffff-ffff-ffffffffffff", "all")
	if err != nil || empty.Data == nil || len(empty.Data) != 0 {
		t.Fatal("empty suffix", err)
	}
	for _, wid := range []string{f.z.ID, "bad", "00000000-0000-0000-0000-000000000000"} {
		_, err = s.ListInvitations(ctx, f.od, wid, true, 50, "", "all")
		requireInvitationError(t, err, workspace.ErrNotFound)
	}
	// A member of the other tenant cannot enumerate that tenant's invitations.
	_, err = s.ListInvitations(ctx, f.md, f.z.ID, true, 50, "", "all")
	requireInvitationError(t, err, workspace.ErrInvitationForbidden)
	p, err = s.ListInvitations(ctx, f.md, "", false, 50, j.ID, "all")
	if err != nil || len(p.Data) != 0 {
		t.Fatal("foreign cursor used as authority")
	}
	expireInvitation(t, db, i.ID)
	p, err = s.ListInvitations(ctx, f.md, "", false, 50, "", "pending")
	if err != nil || len(p.Data) != 0 {
		t.Fatal("expired pending visible")
	}
	p, err = s.ListInvitations(ctx, f.md, "", false, 50, "", "all")
	if err != nil || len(p.Data) != 1 {
		t.Fatal("expired history", err)
	}
	row := p.Data[0].(workspace.RecipientInvitation)
	if row.Status != "expired" || row.WorkspaceName != nil || !row.UpdatedAt.Equal(row.ExpiresAt) {
		t.Fatal("effective expiry projection")
	}
	if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='pending'`, i.ID) != 1 {
		t.Fatal("GET persisted expiry")
	}
	// Candidate Owner check prevents even target-user protection for nonowners.
	stages := []string{}
	observed := invitationObserved(db, func(_ context.Context, stage string) { stages = append(stages, stage) })
	_, err = observed.CreateInvitation(ctx, f.md, f.z.ID, f.owner.ID)
	requireInvitationError(t, err, workspace.ErrInvitationForbidden)
	if count := strings.Count(strings.Join(stages, ","), "user"); count != 1 {
		t.Fatal("nonowner inspected target user", stages)
	}
	for _, a := range []string{"accept", "decline", "revoke"} {
		for _, id := range []string{"bad", strings.ToUpper(i.ID), "00000000-0000-0000-0000-000000000000", j.ID} {
			_, err = s.ActOnInvitation(ctx, f.md, id, a)
			requireInvitationError(t, err, workspace.ErrInvitationNotFound)
		}
	}
}
func TestInvitationAcceptanceVisibilityAndIndependentTenant(t *testing.T) {
	db := workspaceDB(t)
	f := invitationFixture(t, db)
	i := createInvitation(t, db, f)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	p := newWorkspacePause(t)
	observed := invitationObserved(db, func(ctx context.Context, stage string) {
		if stage == "join" {
			p.stop(ctx)
		}
	})
	done := startInvitation(ctx, observed, f.md, i.ID, "accept")
	p.wait(t)
	if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='accepted'`, i.ID) != 0 || queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 0 {
		t.Fatal("uncommitted half visible")
	}
	// Unrelated Workspace work need not wait behind this Workspace's write lock.
	if _, err := (workspace.Store{Pool: db}).Create(ctx, f.xd, "independent tenant"); err != nil {
		t.Fatal("global tenant lock", err)
	}
	p.resume()
	awaitMember(t, done, nil)
	if queryInt(t, db, `SELECT count(*) FROM workspace_invitations WHERE id=$1 AND status='accepted'`, i.ID) != 1 || queryInt(t, db, `SELECT count(*) FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID) != 1 {
		t.Fatal("atomic publication")
	}
}

func mustInvitationExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal("invitation fixture mutation", err)
	}
}
func TestInvitationIndexPlans(t *testing.T) {
	db := workspaceDB(t)
	f := invitationFixture(t, db)
	createInvitation(t, db, f)
	fillPending(t, db, f, 99)
	for name, sql := range map[string]string{
		"workspace": `EXPLAIN (ANALYZE,FORMAT JSON) SELECT id FROM workspace_invitations WHERE workspace_id=$1 AND status='pending' AND expires_at>statement_timestamp() ORDER BY id LIMIT 101`,
		"recipient": `EXPLAIN (ANALYZE,FORMAT JSON) SELECT id FROM workspace_invitations WHERE invitee_user_id=$1 AND status='pending' AND expires_at>statement_timestamp() ORDER BY id LIMIT 101`,
		"capacity":  `EXPLAIN (ANALYZE,FORMAT JSON) SELECT count(*) FROM workspace_invitations WHERE workspace_id=$1 AND status='pending' AND expires_at>statement_timestamp()`} {
		arg := f.w.ID
		if name == "recipient" {
			arg = f.member.ID
		}
		var raw []byte
		if err := db.QueryRow(context.Background(), sql, arg).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plan []map[string]any
		if json.Unmarshal(raw, &plan) != nil {
			t.Fatal("invalid real PG plan")
		}
		node := plan[0]["Plan"].(map[string]any)
		if name != "capacity" && (node["Node Type"] != "Limit" || node["Actual Rows"].(float64) > 100) {
			t.Fatal("unbounded plan")
		}
		t.Logf("PG18 %s plan root=%v actual_rows=%v", name, node["Node Type"], node["Actual Rows"])
	}
}
