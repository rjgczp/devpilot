//go:build integration

package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func monitorInput(name string) monitor.Input {
	u := "https://example.com/private-path"
	return monitor.Input{Name: &name, URL: &u}
}
func monitorFixture(t *testing.T, db *pgxpool.Pool) (memberFixture, monitor.Public) {
	t.Helper()
	f, p := projectFixture(t, db)
	m, e := (monitor.Store{Pool: db}).Create(context.Background(), f.od, f.w.ID, p.ID, monitorInput("monitor"))
	if e != nil {
		t.Fatal(e)
	}
	return f, m
}
func monitorWant(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v want %v", err, want)
	}
}
func monitorHistory(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var p string
	if db.QueryRow(context.Background(), `SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY id)::text,'[]') FROM projects p`).Scan(&p) != nil {
		t.Fatal("project history")
	}
	return projectHistory(t, db) + p
}
func TestMonitorMigration(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 6", true)
	goose(t, dir, "up-to 6", true)
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 6 {
		t.Fatal("fresh6")
	}
	goose(t, dir, "down", true)
	f, p := projectFixture(t, db)
	for _, state := range []string{"pending", "accepted", "declined", "revoked", "expired"} {
		if _, e := db.Exec(ctx, `INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id,status) VALUES($1,$2,$3,$4)`, f.w.ID, f.other.ID, f.owner.ID, state); e != nil {
			t.Fatal(e)
		}
	}
	before := monitorHistory(t, db)
	tmp := t.TempDir()
	for _, name := range []string{"00001_create_users.sql", "00002_create_sessions.sql", "00003_create_workspaces.sql", "00004_create_workspace_invitations.sql", "00005_create_projects.sql", "00006_create_http_monitors.sql"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		if strings.HasPrefix(name, "00006") {
			b = []byte(strings.Replace(string(b), "-- +goose Down", "SELECT 1/0;\n-- +goose Down", 1))
		}
		if os.WriteFile(filepath.Join(tmp, name), b, 0600) != nil {
			t.Fatal("migration fixture")
		}
	}
	goose(t, tmp, "up", false)
	if monitorHistory(t, db) != before || queryInt(t, db, `SELECT count(*) FROM pg_class WHERE relname LIKE 'http_monitors%'`) != 0 || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname='http_monitors_immutable_identity'`) != 0 || queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=6 AND is_applied`) != 0 {
		t.Fatal("real6 partial rollback")
	}
	goose(t, dir, "up-to 6", true)
	goose(t, dir, "up-to 6", true)
	if monitorHistory(t, db) != before || queryInt(t, db, `SELECT count(*) FROM http_monitors`) != 0 {
		t.Fatal("upgrade changed history")
	}
	s := monitor.Store{Pool: db}
	m, e := s.Create(ctx, f.od, f.w.ID, p.ID, monitorInput("配置"))
	if e != nil {
		t.Fatal(e)
	}
	if m.ID[14] != '7' || m.Version != "1" || m.Method != "GET" || m.IntervalSeconds != 60 || m.TimeoutSeconds != 10 || m.ExpectedStatus != 200 || !m.CreatedAt.Equal(m.UpdatedAt) {
		t.Fatal("defaults")
	}
	for _, set := range []string{"id=uuidv7()", "workspace_id='" + f.z.ID + "'", "project_id=uuidv7()", "created_by_user_id='" + f.member.ID + "'", "created_at=created_at+interval '1s'", "name=''", "name=repeat('a',101)", "name=E'x\\n'", "name=' x'", "url=''", "url=repeat('x',2049)", "url='ftp://example.com/'", "url=E'https://example.com/x\\n'", "url='https://中.com/'", "method='POST'", "interval_seconds=59", "timeout_seconds=0", "timeout_seconds=31", "expected_status=199", "expected_status=600", "version=0", "updated_at=created_at-interval '1s'"} {
		if _, e = db.Exec(ctx, "UPDATE http_monitors SET "+set+" WHERE id=$1", m.ID); e == nil {
			t.Fatal("invalid SQL accepted", set)
		}
	}
	columns := []string{"id", "workspace_id", "project_id", "name", "url", "method", "interval_seconds", "timeout_seconds", "expected_status", "created_by_user_id", "version", "created_at", "updated_at"}
	for i := range columns {
		values := append([]string(nil), columns...)
		values[0] = "uuidv7()"
		values[i] = "NULL"
		if _, e = db.Exec(ctx, "INSERT INTO http_monitors("+strings.Join(columns, ",")+") SELECT "+strings.Join(values, ",")+" FROM http_monitors WHERE id=$1", m.ID); e == nil {
			t.Fatal("null accepted", columns[i])
		}
	}
	p2, e := (project.Store{Pool: db}).Create(ctx, f.od, f.w.ID, projectInput("other"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE http_monitors SET project_id=$1 WHERE id=$2`, p2.ID, m.ID); e == nil {
		t.Fatal("same tenant transfer")
	}
	if _, e = db.Exec(ctx, `UPDATE http_monitors SET id=id,workspace_id=workspace_id,project_id=project_id,created_by_user_id=created_by_user_id,created_at=created_at WHERE id=$1`, m.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) VALUES($1,$2,'x','https://example.com/',$3)`, f.z.ID, p.ID, f.owner.ID); e == nil {
		t.Fatal("mixed FK")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_indexes WHERE tablename='http_monitors'`) != 3 || queryInt(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name='http_monitors'`) != 13 {
		t.Fatal("schema set")
	}
	if _, e = db.Exec(ctx, `CREATE TABLE monitor_children_test(workspace_id uuid NOT NULL,project_id uuid NOT NULL,monitor_id uuid NOT NULL,FOREIGN KEY(workspace_id,project_id,monitor_id) REFERENCES http_monitors(workspace_id,project_id,id))`); e != nil {
		t.Fatal(e)
	}
	for _, pair := range [][2]string{{f.z.ID, p.ID}, {f.w.ID, p2.ID}} {
		if _, e = db.Exec(ctx, `INSERT INTO monitor_children_test VALUES($1,$2,$3)`, pair[0], pair[1], m.ID); e == nil {
			t.Fatal("mixed child FK")
		}
	}
	if _, e = db.Exec(ctx, `INSERT INTO monitor_children_test VALUES($1,$2,$3)`, f.w.ID, p.ID, m.ID); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`DELETE FROM http_monitors WHERE id=$1`, `DELETE FROM projects WHERE id=$1`} {
		id := m.ID
		if strings.Contains(q, "projects") {
			id = p.ID
		}
		if _, e = db.Exec(ctx, q, id); e == nil {
			t.Fatal("parent delete allowed")
		}
	}
	if _, e = db.Exec(ctx, `DROP TABLE monitor_children_test`); e != nil {
		t.Fatal(e)
	}
	before = monitorHistory(t, db)
	goose(t, dir, "down", true)
	if monitorHistory(t, db) != before || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname='http_monitors_immutable_identity'`) != 0 {
		t.Fatal("down damaged history")
	}
	goose(t, dir, "up-to 6", true)
	if queryInt(t, db, `SELECT count(*) FROM http_monitors`) != 0 {
		t.Fatal("down up restored deleted data")
	}
}
func TestMonitorStorePermissionsPaginationAndVersions(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	for _, d := range []session.Digest{f.od, f.md} {
		got, e := s.Detail(ctx, d, m.WorkspaceID, m.ProjectID, m.ID)
		if e != nil || got != m {
			t.Fatal("detail", e)
		}
	}
	_, e := s.Create(ctx, f.md, m.WorkspaceID, m.ProjectID, monitorInput("denied"))
	monitorWant(t, e, monitor.ErrForbidden)
	_, e = s.Update(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("denied"), "")
	monitorWant(t, e, monitor.ErrForbidden)
	for _, scope := range [][3]string{{f.z.ID, m.ProjectID, m.ID}, {f.w.ID, "bad", m.ID}, {f.w.ID, m.ProjectID, "bad"}, {"bad", m.ProjectID, m.ID}} {
		_, e = s.Detail(ctx, f.md, scope[0], scope[1], scope[2])
		monitorWant(t, e, monitor.ErrNotFound)
		_, e = s.Update(ctx, f.md, scope[0], scope[1], scope[2], monitorInput("denied"), "")
		monitorWant(t, e, monitor.ErrNotFound)
	}
	_, e = s.Detail(ctx, session.Digest{}, "bad", "bad", "bad")
	monitorWant(t, e, session.ErrInvalid)
	_, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("x"), "")
	monitorWant(t, e, monitor.ErrPreconditionRequired)
	for i := 0; i < 2; i++ {
		if _, e = s.Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("monitor")); e != nil {
			t.Fatal(e)
		}
	}
	page, e := s.List(ctx, f.md, m.WorkspaceID, m.ProjectID, 2, "")
	if e != nil || len(page.Data) != 2 || page.Pagination.NextAfter == nil {
		t.Fatal("page", e)
	}
	next, e := s.List(ctx, f.md, m.WorkspaceID, m.ProjectID, 2, *page.Pagination.NextAfter)
	if e != nil || len(next.Data) != 1 || next.Pagination.NextAfter != nil {
		t.Fatal("next", e)
	}
	tag := monitor.ETag(m)
	for _, name := range []string{"B", "monitor", "monitor"} {
		n := name
		m, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &n}, monitor.ETag(m))
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("old"), tag)
	monitorWant(t, e, monitor.ErrPreconditionFailed)
	if _, e = db.Exec(ctx, `ALTER TABLE http_monitors DISABLE TRIGGER monitor_configuration_revision_guard; UPDATE http_monitors SET version=9223372036854775807 WHERE id='`+m.ID+`'; ALTER TABLE http_monitors ENABLE TRIGGER monitor_configuration_revision_guard`); e != nil {
		t.Fatal(e)
	}
	m.Version = "9223372036854775807"
	_, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("overflow"), monitor.ETag(m))
	monitorWant(t, e, user.ErrInternal)
	if queryInt(t, db, `SELECT count(*) FROM projects WHERE id=$1 AND version=1`, m.ProjectID) != 1 {
		t.Fatal("parent version changed")
	}
	if e = (workspace.Store{Pool: db}).DeleteMember(ctx, f.od, f.w.ID, f.member.ID, f.condition(f.w.ID, f.member.ID)); e != nil {
		t.Fatal(e)
	}
	_, e = s.Detail(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID)
	monitorWant(t, e, monitor.ErrNotFound)
	if _, e = db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.od[:]); e != nil {
		t.Fatal(e)
	}
	_, e = s.Detail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
	monitorWant(t, e, session.ErrInvalid)
	if queryInt(t, db, `SELECT count(*) FROM http_monitors`) != 3 {
		t.Fatal("loss deleted config")
	}
}

func TestMonitorCreatorReferenceAndEmptyScope(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	ctx := context.Background()
	s := monitor.Store{Pool: db}
	empty, e := (project.Store{Pool: db}).Create(ctx, f.od, f.w.ID, projectInput("empty"))
	if e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, f.md, f.w.ID, empty.ID, 50, "")
	if e != nil || page.Data == nil || len(page.Data) != 0 {
		t.Fatal("empty scope", e)
	}
	_, e = s.List(ctx, f.md, f.z.ID, empty.ID, 50, "")
	monitorWant(t, e, monitor.ErrNotFound)
	other, e := (project.Store{Pool: db}).Create(ctx, f.xd, f.z.ID, projectInput("foreign"))
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := s.Create(ctx, f.xd, f.z.ID, other.ID, monitorInput("foreign"))
	if e != nil {
		t.Fatal(e)
	}
	for _, triple := range [][3]string{{f.w.ID, other.ID, foreign.ID}, {f.w.ID, m.ProjectID, foreign.ID}, {f.z.ID, other.ID, m.ID}} {
		_, e = s.Detail(ctx, f.md, triple[0], triple[1], triple[2])
		monitorWant(t, e, monitor.ErrNotFound)
	}
	// Isolate the creator FK using a User with no Session/Membership/Project refs.
	creator, e := (user.Store{Pool: db}).Create(ctx, "monitor-source-only@example.com", "fixture-hash")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) VALUES($1,$2,'source','https://example.com/',$3)`, f.w.ID, m.ProjectID, creator.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `DELETE FROM users WHERE id=$1`, creator.ID); e == nil {
		t.Fatal("creator RESTRICT bypassed")
	}
	if _, e = db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); e != nil {
		t.Fatal(e)
	}
	_, e = s.Detail(ctx, f.od, f.w.ID, m.ProjectID, m.ID)
	monitorWant(t, e, session.ErrInvalid)
	if _, e = s.Detail(ctx, f.md, f.w.ID, m.ProjectID, m.ID); e != nil {
		t.Fatal("creator disable destroyed config", e)
	}
}

func TestMonitorMergedConfigurationRevalidated(t *testing.T) {
	db := workspaceDB(t)
	f, m := monitorFixture(t, db)
	ctx := context.Background()
	s := monitor.Store{Pool: db}
	// SQL CHECK is intentionally narrower than the application URL policy. A
	// supported PATCH must not inherit an invalid config from maintenance SQL.
	if _, e := db.Exec(ctx, `UPDATE http_monitors SET url='https://example.com/?private-token',version=version+1 WHERE id=$1`, m.ID); e != nil {
		t.Fatal(e)
	}
	m.Version = "2" // Maintenance fixture consumed a revision; test current precondition.
	n := "rename"
	_, e := s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &n}, monitor.ETag(m))
	monitorWant(t, e, user.ErrInternal)
	_, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &n}, "")
	monitorWant(t, e, monitor.ErrPreconditionRequired)
	_, e = s.Update(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &n}, monitor.ETag(m))
	monitorWant(t, e, monitor.ErrForbidden)
	if queryInt(t, db, `SELECT count(*) FROM http_monitors WHERE id=$1 AND version=2 AND name='monitor'`, m.ID) != 1 {
		t.Fatal("invalid merged config mutated")
	}
	// Supplying a valid replacement repairs the configuration under its old tag.
	if _, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("repaired"), monitor.ETag(m)); e != nil {
		t.Fatal(e)
	}
}
