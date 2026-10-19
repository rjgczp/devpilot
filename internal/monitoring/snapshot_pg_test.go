//go:build integration && executionsnapshot

package monitoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/testdb"
	"github.com/rjgczp/devpilot/internal/user"
)

func snapshotDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db, err := testdb.Open(context.Background(), os.Getenv("TEST_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var version int
	if db.QueryRow(context.Background(), `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version) != nil || version != 6 {
		t.Fatal("isolated schema6 required; run pinned Goose explicitly")
	}
	return db
}

func TestSystemSnapshotPGAuthorityIsNotCookieAuthority(t *testing.T) {
	db := snapshotDB(t)
	reader := SnapshotReader{Pool: snapshotRole(t, db)}
	ctx := context.Background()
	for _, action := range []string{"exit", "revoked", "disabled", "cleanup"} {
		t.Run(action, func(t *testing.T) {
			f := snapshotFixture(t, db)
			old, err := reader.Read(ctx, f.scope)
			if err != nil {
				t.Fatal(err)
			}
			q := map[string]string{"exit": `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, "revoked": `UPDATE sessions SET revoked_at=statement_timestamp() WHERE user_id=$2`, "disabled": `UPDATE users SET status='disabled' WHERE id=$2`, "cleanup": `DELETE FROM sessions WHERE user_id=$2`}[action]
			// Keep parameter typing explicit for guarded administrator fixtures.
			if action != "exit" {
				q = strings.ReplaceAll(q, "$2", "$1")
				_, err = db.Exec(ctx, q, f.uid)
			} else {
				_, err = db.Exec(ctx, q, f.scope.WorkspaceID, f.uid)
			}
			if err != nil {
				t.Fatal("authority fixture")
			}
			_, err = (monitor.Store{Pool: db}).Detail(ctx, f.digest, f.scope.WorkspaceID, f.scope.ProjectID, f.scope.MonitorID)
			want := session.ErrInvalid
			if action == "exit" {
				if !errors.Is(err, monitor.ErrNotFound) {
					t.Fatal("old Cookie retained membership")
				}
			} else if !errors.Is(err, want) {
				t.Fatal("old Cookie retained identity")
			}
			newer, err := reader.Read(ctx, f.scope)
			if err != nil || newer != old {
				t.Fatal("trusted system reader depends on Cookie lifetime")
			}
		})
	}
}

func TestSystemSnapshotPGLiveIsolatedExecution(t *testing.T) {
	tools := os.Getenv("EXECUTION_TEST_TOOLS")
	evidence := os.Getenv("EXECUTION_PG_EVIDENCE")
	if !filepath.IsAbs(tools) || !filepath.IsAbs(evidence) {
		t.Fatal("controlled Linux binary/evidence paths required; do not skip live PG/network acceptance")
	}
	if os.MkdirAll(evidence, 0755) != nil {
		t.Fatal("evidence directory")
	}
	if files, err := os.ReadDir(evidence); err != nil || len(files) != 0 {
		t.Fatal("fresh dedicated PG/network evidence directory required")
	}
	db := snapshotDB(t)
	role := snapshotRole(t, db)
	f := snapshotFixture(t, db)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `UPDATE http_monitors SET url='http://hold-pg.example.com/private-sentinel',timeout_seconds=3 WHERE id=$1`, f.scope.MonitorID); err != nil {
		t.Fatal("controlled target fixture")
	}
	old, err := (SnapshotReader{Pool: role}).Read(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	runner := filepath.Join(filepath.Dir(file), "../../tests/execution")
	args := []string{"run", "-d", "--network", "none", "--cap-add", "NET_ADMIN", "--cap-add", "SYS_PTRACE", "--sysctl", "net.ipv6.conf.all.disable_ipv6=0", "--memory=128m", "--cpus=1", "--pids-limit=128", "--ulimit", "nofile=256:256", "-v", tools + ":/toolbin:ro", "-v", runner + ":/runner:ro", "-v", evidence + ":/evidence", "-e", "EXECUTION_ISOLATED=1", "-e", "EXECUTION_MODE=pg", "devpilot-execution-audit", "-c", "python3 /runner/run.py"}
	b, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatal("isolated live fixture start")
	}
	cid := strings.TrimSpace(string(b))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", cid).Run() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(evidence, "pg-ready")); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e := os.Stat(filepath.Join(evidence, "pg-ready")); e != nil {
		logs, _ := exec.Command("docker", "logs", cid).CombinedOutput()
		t.Fatal("fixture startup", string(logs))
	}
	runProbe := func(s Snapshot, want Failure, label string) (*exec.Cmd, *bytes.Buffer) {
		cmd := exec.Command("docker", "exec", "-i", cid, "strace", "-f", "-qq", "-yy", "-s", "256", "-e", "trace=process,connect,sendto,sendmsg,sendmmsg", "-o", "/evidence/"+label+".trace", "python3", "-I", "-S", "/runner/drop_probe.py", "pg")
		wire, _ := json.Marshal(wireSnapshot(s, want))
		cmd.Stdin = bytes.NewReader(wire)
		var output bytes.Buffer
		cmd.Stdout = &output
		cmd.Stderr = &output
		if err := cmd.Start(); err != nil {
			t.Fatal("probe start")
		}
		return cmd, &output
	}
	child, output := runProbe(old, "", "v7")
	// The independent receiving endpoint, not a sleep, proves HTTP is in flight.
	deadline = time.Now().Add(2 * time.Second)
	arrived := false
	for time.Now().Before(deadline) {
		events, _ := os.ReadFile(filepath.Join(evidence, "events.jsonl"))
		if bytes.Contains(events, []byte(`"host": "hold-pg.example.com"`)) && bytes.Contains(events, []byte(`"kind": "http"`)) {
			arrived = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !arrived {
		t.Fatal("in-flight HTTP barrier")
	}
	if role.Stat().AcquiredConns() != 0 {
		t.Fatal("DB connection held during network")
	}
	var locks int
	if db.QueryRow(ctx, `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.usename=$1 AND l.relation='http_monitors'::regclass`, role.Config().ConnConfig.User).Scan(&locks) != nil || locks != 0 {
		t.Fatal("probe retained DB lock")
	}
	newURL := "http://pg-new.example.com/new-sentinel"
	timeout, status := int64(1), int64(201)
	updateCtx, cancel := context.WithTimeout(ctx, time.Second)
	_, err = (monitor.Store{Pool: db}).Update(updateCtx, f.digest, f.scope.WorkspaceID, f.scope.ProjectID, f.scope.MonitorID, monitor.Input{URL: &newURL, TimeoutSeconds: &timeout, ExpectedStatus: &status}, `"monitor:`+f.scope.MonitorID+`:7"`)
	cancel()
	if err != nil {
		t.Fatal("slow network blocked Owner PATCH")
	}
	newer, err := (SnapshotReader{Pool: role}).Read(ctx, f.scope)
	if err != nil || newer.version != 8 || newer.timeout != 1 || newer.expected != 201 {
		t.Fatal("new version")
	}
	if _, err = db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.digest[:]); err != nil {
		t.Fatal("revocation blocked")
	}
	if _, err = (monitor.Store{Pool: db}).Detail(ctx, f.digest, f.scope.WorkspaceID, f.scope.ProjectID, f.scope.MonitorID); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("revoked Cookie still authorized")
	}
	// Delay the fixture beyond the NEW one-second timeout; the old execution
	// must retain its original three-second deadline and expected status200.
	time.Sleep(1200 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(evidence, "release-pg"), []byte("release"), 0644)
	if err = child.Wait(); err != nil {
		t.Fatal("old execution failed", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(`"configuration_version":"7"`)) || !bytes.Contains(output.Bytes(), []byte(`"success":true`)) {
		t.Fatal("old execution published new version")
	}
	_ = os.WriteFile(filepath.Join(evidence, "v7.txt"), output.Bytes(), 0644)
	child, output = runProbe(newer, UnexpectedStatus, "v8")
	if err = child.Wait(); err != nil {
		t.Fatal("new execution failed", output.String())
	}
	if !bytes.Contains(output.Bytes(), []byte(`"configuration_version":"8"`)) || !bytes.Contains(output.Bytes(), []byte(`"failure_code":"unexpected_status"`)) {
		t.Fatal("new result lineage")
	}
	_ = os.WriteFile(filepath.Join(evidence, "v8.txt"), output.Bytes(), 0644)
	for _, label := range []string{"v7", "v8"} {
		audit, err := exec.Command("docker", "exec", cid, "python3", "/runner/audit.py", "/evidence/"+label+".trace").CombinedOutput()
		_ = os.WriteFile(filepath.Join(evidence, label+".audit.json"), audit, 0644)
		if err != nil {
			t.Fatal("live execution audit failed")
		}
	}
	_ = os.WriteFile(filepath.Join(evidence, "stop-pg"), []byte("stop"), 0644)
	t.Log("PG18 live proof: v7 HTTP in flight; pool released, zero Monitor locks; protected Owner v8 PATCH and revocation complete; v7 retains old 3s/200 after >new1s; v8 gets own result; child has no DB/Cookie credentials")
}

type pgFixture struct {
	scope  Scope
	uid    string
	digest session.Digest
}

func snapshotFixture(t *testing.T, db *pgxpool.Pool) pgFixture {
	t.Helper()
	ctx := context.Background()
	var f pgFixture
	u, err := (user.Store{Pool: db}).Create(ctx, fmt.Sprintf("snapshot-%d@example.com", time.Now().UnixNano()), password.DummyPHC)
	if err != nil {
		t.Fatal("user fixture")
	}
	f.uid = u.ID
	_, f.digest, err = session.NewTokens().New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (session.Store{Pool: db}).Create(ctx, user.Credentials{Public: u, PasswordHash: password.DummyPHC}, f.digest, time.Hour); err != nil {
		t.Fatal("session fixture")
	}
	if db.QueryRow(ctx, `INSERT INTO workspaces(name) VALUES('snapshot fixture') RETURNING id::text`).Scan(&f.scope.WorkspaceID) != nil {
		t.Fatal("workspace fixture")
	}
	if _, err = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, f.scope.WorkspaceID, f.uid); err != nil {
		t.Fatal("owner fixture")
	}
	if db.QueryRow(ctx, `INSERT INTO projects(workspace_id,name,created_by_user_id) VALUES($1,'fixture',$2) RETURNING id::text`, f.scope.WorkspaceID, f.uid).Scan(&f.scope.ProjectID) != nil {
		t.Fatal("project fixture")
	}
	if db.QueryRow(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,timeout_seconds,version,created_by_user_id) VALUES($1,$2,'fixture','http://probe-a.example.com/secret-sentinel',1,7,$3) RETURNING id::text`, f.scope.WorkspaceID, f.scope.ProjectID, f.uid).Scan(&f.scope.MonitorID) != nil {
		t.Fatal("monitor fixture")
	}
	return f
}
func snapshotRole(t *testing.T, db *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("probe_snapshot_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err := db.Exec(ctx, "CREATE ROLE "+ident+" LOGIN PASSWORD 'fixture_only_read' NOINHERIT NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE"); err != nil {
		t.Fatal("create fixture role")
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, "DROP OWNED BY "+ident)
		_, err := db.Exec(ctx, "DROP ROLE "+ident)
		if err != nil {
			t.Error("role cleanup")
		}
	})
	// PostgreSQL grants TEMP through PUBLIC by default. Remove it only in this
	// disposable database and restore the original public ACL during cleanup.
	var publicTemp bool
	if db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database d,LATERAL aclexplode(COALESCE(d.datacl,acldefault('d',d.datdba))) a WHERE d.datname=current_database() AND a.grantee=0 AND a.privilege_type='TEMPORARY')`).Scan(&publicTemp) != nil {
		t.Fatal("database ACL")
	}
	if _, err := db.Exec(ctx, `REVOKE TEMP ON DATABASE devpilot_registration_test FROM PUBLIC`); err != nil {
		t.Fatal("temp fixture ACL")
	}
	t.Cleanup(func() {
		if publicTemp {
			_, _ = db.Exec(ctx, `GRANT TEMP ON DATABASE devpilot_registration_test TO PUBLIC`)
		}
	})
	for _, q := range []string{"GRANT CONNECT ON DATABASE devpilot_registration_test TO " + ident, "GRANT USAGE ON SCHEMA public TO " + ident,
		"GRANT SELECT(id) ON workspaces TO " + ident, "GRANT SELECT(id,workspace_id) ON projects TO " + ident,
		"GRANT SELECT(id,workspace_id,project_id,name,url,method,interval_seconds,timeout_seconds,expected_status,version) ON http_monitors TO " + ident} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatal("fixture grant")
		}
	}
	cfg := db.Config().Copy()
	cfg.ConnConfig.User = name
	cfg.ConnConfig.Password = "fixture_only_read"
	cfg.MaxConns = 1
	role, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("fixture pool")
	}
	t.Cleanup(role.Close)
	return role
}

func TestSystemSnapshotPGScopePrivilegesAndRelease(t *testing.T) {
	db := snapshotDB(t)
	a, b := snapshotFixture(t, db), snapshotFixture(t, db)
	role := snapshotRole(t, db)
	reader := SnapshotReader{Pool: role}
	ctx := context.Background()
	s, err := reader.Read(ctx, a.scope)
	if err != nil || s.version != 7 || s.scope != a.scope || s.timeout != 1 {
		t.Fatal("snapshot")
	}
	if role.Stat().AcquiredConns() != 0 {
		t.Fatal("reader retained pool connection")
	}
	for _, scope := range []Scope{{MonitorID: a.scope.MonitorID}, {b.scope.WorkspaceID, a.scope.ProjectID, a.scope.MonitorID}, {a.scope.WorkspaceID, b.scope.ProjectID, a.scope.MonitorID}, {a.scope.WorkspaceID, a.scope.ProjectID, b.scope.MonitorID}} {
		if _, err = reader.Read(ctx, scope); !errors.Is(err, ErrSnapshotUnavailable) {
			t.Fatal("mixed scope")
		}
	}
	for _, q := range []string{`SELECT * FROM users`, `SELECT * FROM sessions`, `SELECT * FROM workspace_members`, `UPDATE http_monitors SET name='bad'`, `DELETE FROM projects`, `CREATE TABLE public.forbidden(id int)`, `CREATE TEMP TABLE forbidden(id int)`, `ALTER TABLE http_monitors ADD forbidden text`, `SET ROLE devpilot_test`} {
		if _, err := role.Exec(ctx, q); err == nil {
			t.Fatal("unapproved fixture privilege", q)
		}
	}
	if _, err = db.Exec(ctx, `UPDATE http_monitors SET url='http://127.0.0.1/secret-sentinel' WHERE id=$1`, a.scope.MonitorID); err != nil {
		t.Fatal("DB subset fixture")
	}
	if _, err = reader.Read(ctx, a.scope); !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatal("offline guard")
	}
	// The statement is finished and its locks gone; the independent admin can
	// acquire an exclusive table lock immediately, not after a network timeout.
	tx, _ := db.Begin(ctx)
	defer tx.Rollback(ctx)
	_, _ = tx.Exec(ctx, `SET LOCAL lock_timeout='200ms'`)
	if _, err = tx.Exec(ctx, `LOCK TABLE http_monitors IN ACCESS EXCLUSIVE MODE NOWAIT`); err != nil {
		t.Fatal("snapshot retained transaction lock")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	conn, _ := role.Acquire(ctx)
	start := time.Now()
	_, err = reader.Read(ctx, b.scope)
	conn.Release()
	if !errors.Is(err, ErrSnapshotUnavailable) || time.Since(start) > 2600*time.Millisecond {
		t.Fatal("two second pool wait budget")
	}
	t.Log("PG18: actual LOGIN role, complete scope, denied writes/DDL/identity reads, released pool/locks, 2s deadline")
}

func TestSystemSnapshotPGStatementConsistency(t *testing.T) {
	db := snapshotDB(t)
	f := snapshotFixture(t, db)
	role := snapshotRole(t, db)
	reader := SnapshotReader{Pool: role}
	ctx := context.Background()
	old, err := reader.Read(ctx, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	writer, _ := db.Begin(ctx)
	defer writer.Rollback(ctx)
	if _, err = writer.Exec(ctx, `UPDATE http_monitors SET url='https://new.example.com/new-sentinel',timeout_seconds=2,expected_status=302,version=8 WHERE workspace_id=$1 AND project_id=$2 AND id=$3`, f.scope.WorkspaceID, f.scope.ProjectID, f.scope.MonitorID); err != nil {
		t.Fatal("writer")
	}
	uncommitted, err := reader.Read(ctx, f.scope)
	if err != nil || uncommitted != old {
		t.Fatal("uncommitted update leaked")
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	newer, err := reader.Read(ctx, f.scope)
	if err != nil || newer.version != 8 || newer.timeout != 2 || newer.expected != 302 || newer.target != "https://new.example.com/new-sentinel" || old.version != 7 || old.timeout != 1 {
		t.Fatal("torn version")
	}
	// Use the supported conditional writer as well; the pure system reader has
	// neither a cookie gate nor any power to write through this user protocol.
	name := "changed"
	_, err = (monitor.Store{Pool: db}).Update(ctx, f.digest, f.scope.WorkspaceID, f.scope.ProjectID, f.scope.MonitorID, monitor.Input{Name: &name}, `"monitor:`+f.scope.MonitorID+`:8"`)
	if err != nil {
		t.Fatal("protected Owner PATCH")
	}
	latest, err := reader.Read(ctx, f.scope)
	if err != nil || latest.version != 9 || latest.target != newer.target {
		t.Fatal("protected writer composition")
	}
	if strings.Contains(fmt.Sprintf("%#v", latest), "sentinel") {
		t.Fatal("snapshot leak")
	}
	t.Log("PG18: independent writer transaction barriers prove complete old/v8 snapshots; supported Owner update reaches v9")
}
