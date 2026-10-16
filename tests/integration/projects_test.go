//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func projectInput(name string) project.Input { return project.Input{Name: &name} }
func projectFixture(t *testing.T, db *pgxpool.Pool) (memberFixture, project.Public) {
	t.Helper()
	f := membersFixture(t, db)
	p, err := (project.Store{Pool: db}).Create(context.Background(), f.od, f.w.ID, projectInput("project"))
	if err != nil {
		t.Fatal(err)
	}
	return f, p
}
func projectWant(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v want %v", err, want)
	}
}
func projectHistory(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var s string
	if err := db.QueryRow(context.Background(), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM users x),(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM sessions x),(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM workspaces x),(SELECT jsonb_agg(to_jsonb(x) ORDER BY workspace_id,user_id) FROM workspace_members x),(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM workspace_invitations x))::text`).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}
func TestProjectMigration(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 4", true)
	f := membersFixture(t, db)
	// Distinct invitation history states and revoked sessions survive exactly.
	for _, state := range []string{"pending", "accepted", "declined", "revoked", "expired"} {
		if _, err := db.Exec(ctx, `INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id,status) VALUES($1,$2,$3,$4)`, f.w.ID, f.other.ID, f.owner.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$1`, f.other.ID); err != nil {
		t.Fatal(err)
	}
	before := projectHistory(t, db)
	tmp := t.TempDir()
	for _, file := range []string{"00001_create_users.sql", "00002_create_sessions.sql", "00003_create_workspaces.sql", "00004_create_workspace_invitations.sql", "00005_create_projects.sql"} {
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(file, "00005") {
			b = []byte(strings.Replace(string(b), "-- +goose Down", "SELECT 1/0;\n-- +goose Down", 1))
		}
		if os.WriteFile(filepath.Join(tmp, file), b, 0600) != nil {
			t.Fatal("migration fixture")
		}
	}
	goose(t, tmp, "up", false)
	if queryInt(t, db, `SELECT count(*) FROM pg_class WHERE relname LIKE 'projects%'`) != 0 || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname='projects_immutable_identity'`) != 0 || queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=5 AND is_applied`) != 0 || projectHistory(t, db) != before {
		t.Fatal("real v5 failure was not atomic")
	}
	goose(t, dir, "up", true)
	goose(t, dir, "up", true)
	if projectHistory(t, db) != before || queryInt(t, db, `SELECT count(*) FROM projects`) != 0 {
		t.Fatal("upgrade changed history")
	}
	p, err := (project.Store{Pool: db}).Create(ctx, f.od, f.w.ID, projectInput("project"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID[14] != '7' || p.Version != "1" || p.Description != "" || !p.CreatedAt.Equal(p.UpdatedAt) {
		t.Fatal("defaults")
	}
	goose(t, dir, "up", true)
	for _, set := range []string{"id=uuidv7()", "workspace_id='" + f.z.ID + "'", "created_by_user_id='" + f.member.ID + "'", "created_at=created_at+interval '1s'", "name=''", "name=repeat('a',101)", "name=E'x\\n'", "description=repeat('a',2001)", "description=E'x\\r'", "version=0", "version=-1", "workspace_id=NULL", "description=NULL", "updated_at=created_at-interval '1s'"} {
		if _, err = db.Exec(ctx, "UPDATE projects SET "+set+" WHERE id=$1", p.ID); err == nil {
			t.Fatal("invalid project update accepted", set)
		}
	}
	columns := []string{"id", "workspace_id", "name", "description", "created_by_user_id", "created_at", "updated_at", "version"}
	for j := range columns {
		values := []string{"uuidv7()", "workspace_id", "name", "description", "created_by_user_id", "created_at", "updated_at", "version"}
		values[j] = "NULL"
		if _, err = db.Exec(ctx, "INSERT INTO projects("+strings.Join(columns, ",")+") SELECT "+strings.Join(values, ",")+" FROM projects WHERE id=$1", p.ID); err == nil {
			t.Fatal("required column accepted NULL", columns[j])
		}
	}
	if _, err = db.Exec(ctx, `UPDATE projects SET id=id,workspace_id=workspace_id,created_by_user_id=created_by_user_id,created_at=created_at WHERE id=$1`, p.ID); err != nil {
		t.Fatal("equal immutable assignment", err)
	}
	for _, args := range [][]any{{"00000000-0000-0000-0000-000000000000", f.owner.ID}, {f.w.ID, "00000000-0000-0000-0000-000000000000"}} {
		if _, err = db.Exec(ctx, `INSERT INTO projects(workspace_id,name,created_by_user_id) VALUES($1,'x',$2)`, args...); err == nil {
			t.Fatal("unknown FK")
		}
	}
	if _, err = db.Exec(ctx, `CREATE TABLE project_children_test(workspace_id uuid NOT NULL,project_id uuid NOT NULL,FOREIGN KEY(workspace_id,project_id) REFERENCES projects(workspace_id,id))`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO project_children_test VALUES($1,$2)`, f.w.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO project_children_test VALUES($1,$2)`, f.z.ID, p.ID); err == nil {
		t.Fatal("cross tenant FK accepted")
	}
	if _, err = db.Exec(ctx, `DELETE FROM workspaces WHERE id=$1`, f.w.ID); err == nil {
		t.Fatal("workspace deletion must restrict")
	}
	// Remove old creator dependencies to isolate the Project User RESTRICT.
	if _, err = db.Exec(ctx, `DELETE FROM workspace_members WHERE user_id=$1;`, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM workspace_invitations WHERE created_by_user_id=$1 OR invitee_user_id=$1`, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, f.owner.ID); err == nil {
		t.Fatal("creator delete must restrict")
	}
	if _, err = db.Exec(ctx, `DROP TABLE project_children_test`); err != nil {
		t.Fatal(err)
	}
	downBefore := projectHistory(t, db)
	goose(t, dir, "down", true)
	if projectHistory(t, db) != downBefore || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname='projects_immutable_identity'`) != 0 {
		t.Fatal("v5 down harmed history")
	}
	goose(t, dir, "up", true)
	goose(t, dir, "down-to 0", true)
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT count(*) FROM pg_indexes WHERE tablename='projects'`) != 3 {
		t.Fatal("fresh schema/indexes")
	}
}
func TestProjectStorePermissionsPaginationAndVersions(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, p := projectFixture(t, db)
	s := project.Store{Pool: db}
	for _, d := range []session.Digest{f.od, f.md} {
		got, err := s.Detail(ctx, d, f.w.ID, p.ID)
		if err != nil || got != p {
			t.Fatal("detail", got, err)
		}
	}
	_, err := s.Create(ctx, f.md, f.w.ID, projectInput("no"))
	projectWant(t, err, project.ErrForbidden)
	_, err = s.Update(ctx, f.md, f.w.ID, p.ID, projectInput("no"), "")
	projectWant(t, err, project.ErrForbidden)
	for _, pair := range [][2]string{{f.z.ID, p.ID}, {f.w.ID, f.z.ID}, {"bad", p.ID}, {f.w.ID, "BAD"}, {strings.ToUpper(f.w.ID), p.ID}} {
		_, err = s.Detail(ctx, f.md, pair[0], pair[1])
		projectWant(t, err, project.ErrNotFound)
		_, err = s.Update(ctx, f.od, pair[0], pair[1], projectInput("no"), project.ETag(p))
		projectWant(t, err, project.ErrNotFound)
	}
	_, err = s.Detail(ctx, f.xd, f.w.ID, p.ID)
	projectWant(t, err, project.ErrNotFound)
	_, err = s.Detail(ctx, session.Digest{}, "bad", "bad")
	projectWant(t, err, session.ErrInvalid)
	_, err = s.Create(ctx, f.od, f.z.ID, projectInput("no"))
	projectWant(t, err, project.ErrNotFound)
	for i := 0; i < 3; i++ {
		if _, err = s.Create(ctx, f.od, f.w.ID, projectInput("project")); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.List(ctx, f.md, f.w.ID, 2, "")
	if err != nil || len(first.Data) != 2 || first.Pagination.NextAfter == nil {
		t.Fatal("first page", err)
	}
	second, err := s.List(ctx, f.md, f.w.ID, 2, *first.Pagination.NextAfter)
	if err != nil || len(second.Data) != 2 || second.Pagination.NextAfter != nil || second.Data[0].ID <= first.Data[1].ID {
		t.Fatal("second page", err)
	}
	for _, after := range []string{"ffffffff-ffff-ffff-ffff-ffffffffffff", f.z.ID} {
		if _, err = s.List(ctx, f.md, f.w.ID, 2, after); err != nil {
			t.Fatal(err)
		}
	}
	empty, err := s.List(ctx, f.xd, f.z.ID, 50, "")
	if err != nil || empty.Data == nil || len(empty.Data) != 0 {
		t.Fatal("authorized empty", err)
	}
	_, err = s.List(ctx, f.xd, f.w.ID, 50, "")
	projectWant(t, err, project.ErrNotFound)
	_, err = s.Update(ctx, f.od, f.w.ID, p.ID, projectInput("A"), "")
	projectWant(t, err, project.ErrPreconditionRequired)
	original := project.ETag(p)
	for i, name := range []string{"project", "B", "project"} {
		p, err = s.Update(ctx, f.od, f.w.ID, p.ID, projectInput(name), project.ETag(p))
		if err != nil || p.Version != fmt.Sprint(i+2) {
			t.Fatal("version progression", p, err)
		}
	}
	_, err = s.Update(ctx, f.od, f.w.ID, p.ID, projectInput("stale"), original)
	projectWant(t, err, project.ErrPreconditionFailed)
	if _, err = db.Exec(ctx, `UPDATE projects SET version=9223372036854775807 WHERE id=$1`, p.ID); err != nil {
		t.Fatal(err)
	}
	p.Version = "9223372036854775807"
	_, err = s.Update(ctx, f.od, f.w.ID, p.ID, projectInput("overflow"), project.ETag(p))
	projectWant(t, err, user.ErrInternal)
	if queryInt(t, db, `SELECT count(*) FROM projects WHERE id=$1 AND version=9223372036854775807 AND name='project'`, p.ID) != 1 {
		t.Fatal("overflow changed resource")
	}
}
func TestProjectLiveAuthorityAndCreatorLifetime(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, kind := range []string{"exit", "remove", "revoke", "disable", "creator"} {
		t.Run(kind, func(t *testing.T) {
			f, p := projectFixture(t, db)
			s := project.Store{Pool: db}
			ws := workspace.Store{Pool: db}
			switch kind {
			case "exit":
				if err := ws.DeleteMember(ctx, f.md, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil {
					t.Fatal(err)
				}
			case "remove":
				if err := ws.DeleteMember(ctx, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); err != nil {
					t.Fatal(err)
				}
			case "revoke":
				if err := (session.Store{Pool: db}).Revoke(ctx, f.md); err != nil {
					t.Fatal(err)
				}
			case "disable":
				if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.member.ID); err != nil {
					t.Fatal(err)
				}
			case "creator":
				if _, err := db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
					t.Fatal(err)
				}
				if err := ws.DeleteMember(ctx, f.od, f.w.ID, f.owner.ID, f.condition(f.w.ID, f.owner.ID)); err != nil {
					t.Fatal(err)
				}
				_, err := s.Detail(ctx, f.od, f.w.ID, p.ID)
				projectWant(t, err, project.ErrNotFound)
				if _, err = db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); err != nil {
					t.Fatal(err)
				}
				if err = (session.Store{Pool: db}).Revoke(ctx, f.od); err != nil {
					t.Fatal(err)
				}
				if _, err := s.Detail(ctx, f.md, f.w.ID, p.ID); err != nil {
					t.Fatal("creator departure destroyed project", err)
				}
				if _, err := s.Update(ctx, f.md, f.w.ID, p.ID, projectInput("retained"), project.ETag(p)); err != nil {
					t.Fatal(err)
				}
				_, err = s.Detail(ctx, f.od, f.w.ID, p.ID)
				projectWant(t, err, session.ErrInvalid)
				return
			}
			want := project.ErrNotFound
			if kind == "revoke" || kind == "disable" {
				want = session.ErrInvalid
			}
			_, err := s.Detail(ctx, f.md, f.w.ID, p.ID)
			projectWant(t, err, want)
			_, err = s.List(ctx, f.md, f.w.ID, 50, "")
			projectWant(t, err, want)
			_, err = s.Create(ctx, f.md, f.w.ID, projectInput("no"))
			projectWant(t, err, want)
			_, err = s.Update(ctx, f.md, f.w.ID, p.ID, projectInput("no"), project.ETag(p))
			projectWant(t, err, want)
			if kind == "exit" || kind == "remove" {
				i, err := ws.CreateInvitation(ctx, f.od, f.w.ID, f.member.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.Detail(ctx, f.md, f.w.ID, p.ID); !errors.Is(err, project.ErrNotFound) {
					t.Fatal("pending grants access")
				}
				if _, err = ws.ActOnInvitation(ctx, f.md, i.ID, "accept"); err != nil {
					t.Fatal(err)
				}
				if _, err = s.Detail(ctx, f.md, f.w.ID, p.ID); err != nil {
					t.Fatal(err)
				}
				_, err = s.Update(ctx, f.md, f.w.ID, p.ID, projectInput("no"), project.ETag(p))
				projectWant(t, err, project.ErrForbidden)
			}
		})
	}
}
