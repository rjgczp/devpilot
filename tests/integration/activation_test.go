//go:build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func activationRead(t *testing.T, db *pgxpool.Pool, d session.Digest, m monitor.Public) monitor.Activation {
	t.Helper()
	a, e := (monitor.Store{Pool: db}).ActivationDetail(context.Background(), d, m.WorkspaceID, m.ProjectID, m.ID)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func activationPut(s monitor.ActivationRepository, d session.Digest, m monitor.Public, a monitor.Activation, state string) (monitor.Activation, error) {
	return s.ActivationUpdate(context.Background(), d, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: state, ConfigurationVersion: a.ConfigurationVersion}, monitor.ActivationETag(a))
}

// Fixture-only owner SQL seeds otherwise unreachable failures. Production code
// never disables triggers. The enclosing transaction restores protection atomically.
func activationSeed(t *testing.T, db *pgxpool.Pool, m monitor.Public, set string) {
	t.Helper()
	tx, e := db.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	for _, q := range []string{`ALTER TABLE monitor_activation_states DISABLE TRIGGER monitor_activation_guard`, `UPDATE monitor_activation_states SET ` + set + ` WHERE monitor_id='` + m.ID + `'`, `ALTER TABLE monitor_activation_states ENABLE TRIGGER monitor_activation_guard`} {
		if _, e = tx.Exec(context.Background(), q); e != nil {
			t.Fatal(e)
		}
	}
	if tx.Commit(context.Background()) != nil {
		t.Fatal("seed commit")
	}
}
func TestActivationMigration(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 6", true)
	f, m := monitorFixture(t, db)
	for _, state := range []string{"pending", "accepted", "declined", "revoked", "expired"} {
		if _, e := db.Exec(ctx, `INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id,status) VALUES($1,$2,$3,$4)`, f.w.ID, f.other.ID, f.owner.ID, state); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.xd[:]); e != nil {
		t.Fatal(e)
	}
	var before string
	if db.QueryRow(ctx, `SELECT to_jsonb(m)::text FROM http_monitors m WHERE id=$1`, m.ID).Scan(&before) != nil {
		t.Fatal("history")
	}
	prior := monitorHistory(t, db)
	tmp := t.TempDir()
	files, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	for _, p := range files {
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		if strings.Contains(p, "00007_") {
			b = []byte(strings.Replace(string(b), "-- +goose Down", "SELECT 1/0;\n-- +goose Down", 1))
		}
		if os.WriteFile(filepath.Join(tmp, filepath.Base(p)), b, 0600) != nil {
			t.Fatal("copy")
		}
	}
	goose(t, tmp, "up-to 7", false)
	if queryInt(t, db, `SELECT count(*) FROM pg_class WHERE relname LIKE 'monitor_activation%'`) != 0 || queryInt(t, db, `SELECT count(*) FROM pg_proc WHERE proname LIKE 'monitor_activation%' OR proname='monitor_configuration_revision_guard'`) != 0 || queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=7 AND is_applied`) != 0 {
		t.Fatal("partial7")
	}
	goose(t, dir, "up-to 7", true)
	goose(t, dir, "up-to 7", true)
	var after string
	db.QueryRow(ctx, `SELECT to_jsonb(m)::text FROM http_monitors m WHERE id=$1`, m.ID).Scan(&after)
	if before != after || prior != monitorHistory(t, db) {
		t.Fatal("upgrade changed history")
	}
	a, err := (monitor.SchemaSevenFixtureStore{Pool: db}).ActivationDetail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.DesiredState != "disabled" || a.ActivationGeneration != "1" || a.ActivationConfigurationVersion != nil || a.NextDueAt != nil || a.ExecutionState != "blocked" {
		t.Fatal("backfill")
	}
	for _, set := range []string{"desired_state='running'", "desired_state='enabled'", "activation_configuration_version=1", "activation_generation=0", "activation_generation=3", "activation_generation=-1", "next_due_at=statement_timestamp()", "created_at=created_at+interval '1s'", "updated_at=updated_at-interval '1s'", "monitor_id=uuidv7()", "project_id=uuidv7()", "workspace_id=uuidv7()", "last_actor_user_id='" + f.owner.ID + "'", "last_transition_kind='owner_paused'"} {
		if _, e := db.Exec(ctx, "UPDATE monitor_activation_states SET "+set+" WHERE monitor_id=$1", m.ID); e == nil {
			t.Fatal("accepted", set)
		}
	}
	if _, e := db.Exec(ctx, `DELETE FROM monitor_activation_states WHERE monitor_id=$1`, m.ID); e == nil {
		t.Fatal("delete")
	}
	if _, e := db.Exec(ctx, `TRUNCATE monitor_activation_states`); e == nil {
		t.Fatal("truncate")
	}
	if _, e := db.Exec(ctx, `INSERT INTO monitor_activation_states(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, f.z.ID, m.ProjectID, m.ID); e == nil {
		t.Fatal("mixed FK")
	}
	a, e := activationPut(monitor.SchemaSevenFixtureStore{Pool: db}, f.od, m, a, "enabled")
	if e != nil {
		t.Fatal(e)
	}
	for _, set := range []string{"activation_generation=1", "desired_state='disabled',activation_configuration_version=NULL,last_transition_kind='configuration_changed',last_actor_user_id=NULL", "next_enable_allowed_at=NULL,activation_generation=activation_generation+1", "next_enable_allowed_at=next_enable_allowed_at-interval '1s',activation_generation=activation_generation+1"} {
		if _, e := db.Exec(ctx, "UPDATE monitor_activation_states SET "+set+" WHERE monitor_id=$1", m.ID); e == nil {
			t.Fatal("cooling guard", set)
		}
	}
	// Default initialization must roll back the Monitor when its child insert fails.
	if _, e = db.Exec(ctx, `CREATE FUNCTION activation_fixture_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER activation_fixture_fail BEFORE INSERT ON monitor_activation_states FOR EACH ROW EXECUTE FUNCTION activation_fixture_fail()`); e != nil {
		t.Fatal(e)
	}
	n := queryInt(t, db, `SELECT count(*) FROM http_monitors`)
	_, e = (monitor.Store{Pool: db}).Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("rollback"))
	if e == nil || queryInt(t, db, `SELECT count(*) FROM http_monitors`) != n {
		t.Fatal("partial creation")
	}
	if _, e = db.Exec(ctx, `DROP TRIGGER activation_fixture_fail ON monitor_activation_states; DROP FUNCTION activation_fixture_fail()`); e != nil {
		t.Fatal(e)
	}
	fresh, e := (monitor.Store{Pool: db}).Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("fresh"))
	if e != nil {
		t.Fatal(e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_activation_states WHERE monitor_id=$1 AND desired_state='disabled' AND activation_generation=1`, fresh.ID) != 1 {
		t.Fatal("initialize")
	}
	goose(t, dir, "down", true)
	if queryInt(t, db, `SELECT count(*) FROM http_monitors WHERE id=$1`, m.ID) != 1 {
		t.Fatal("down erased config")
	}
	goose(t, dir, "up-to 7", true)
	goose(t, dir, "down-to 0", true)
	goose(t, dir, "up-to 7", true)
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 7 {
		t.Fatal("fresh7")
	}
}
func TestActivationStatePermissionsAndInvalidation(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	for _, d := range []session.Digest{f.md, f.xd} {
		_, e := activationPut(s, d, m, a, "enabled")
		want := monitor.ErrNotFound
		if d == f.md {
			want = monitor.ErrForbidden
		}
		monitorWant(t, e, want)
	}
	_, e := s.ActivationDetail(ctx, session.Digest{}, m.WorkspaceID, m.ProjectID, m.ID)
	monitorWant(t, e, session.ErrInvalid)
	for _, scope := range [][3]string{{f.z.ID, m.ProjectID, m.ID}, {m.WorkspaceID, f.z.ID, m.ID}, {m.WorkspaceID, m.ProjectID, f.z.ID}, {"bad", m.ProjectID, m.ID}} {
		_, e = s.ActivationDetail(ctx, f.od, scope[0], scope[1], scope[2])
		monitorWant(t, e, monitor.ErrNotFound)
	}
	_, e = s.ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "enabled", ConfigurationVersion: "1"}, "")
	monitorWant(t, e, monitor.ErrPreconditionRequired)
	first := a
	a, e = activationPut(s, f.od, m, a, "enabled")
	if e != nil || a.ActivationGeneration != "2" || a.ActivationConfigurationVersion == nil || *a.ActivationConfigurationVersion != "1" {
		t.Fatal("enable", e)
	}
	_, e = activationPut(s, f.od, m, first, "disabled")
	monitorWant(t, e, monitor.ErrPreconditionFailed)
	_, e = activationPut(s, f.od, m, a, "enabled")
	var rate monitor.ActivationRateLimit
	if !errors.As(e, &rate) || rate.RetryAfter < 1 || rate.RetryAfter > 60 {
		t.Fatal("cooldown", e)
	}
	a, e = activationPut(s, f.od, m, a, "disabled")
	if e != nil || a.ActivationGeneration != "3" || a.ActivationConfigurationVersion != nil || a.NextEnableAllowedAt == nil {
		t.Fatal("pause", e)
	}
	a, e = activationPut(s, f.od, m, a, "disabled")
	if e != nil || a.ActivationGeneration != "4" {
		t.Fatal("same pause", e)
	}
	for _, input := range []monitor.Input{{Name: &m.Name}, {URL: &m.URL}, {Method: &m.Method}, {IntervalSeconds: &m.IntervalSeconds}, {TimeoutSeconds: &m.TimeoutSeconds}, {ExpectedStatus: &m.ExpectedStatus}, monitorInput("changed")} {
		old := a
		cool := *a.NextEnableAllowedAt
		m, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ETag(m))
		if e != nil {
			t.Fatal(e)
		}
		a = activationRead(t, db, f.md, m)
		if a.DesiredState != "disabled" || a.ActivationConfigurationVersion != nil || a.LastTransitionKind != "configuration_changed" || !a.NextEnableAllowedAt.Equal(cool) {
			t.Fatal("invalidation")
		}
		_, e = activationPut(s, f.od, m, old, "enabled")
		monitorWant(t, e, monitor.ErrPreconditionFailed)
	}
	// Trusted fixture simulates elapsed database cooling without sleeping 60s.
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	a = activationRead(t, db, f.od, m)
	a, e = activationPut(s, f.od, m, a, "enabled")
	if e != nil {
		t.Fatal("resume", e)
	}
	if _, e = db.Exec(ctx, `UPDATE sessions SET revoked_at=statement_timestamp() WHERE token_hash=$1`, f.od[:]); e != nil {
		t.Fatal(e)
	}
	got := activationRead(t, db, f.md, m)
	if got.ActivationGeneration != a.ActivationGeneration || got.DesiredState != "enabled" {
		t.Fatal("intent lost with session")
	}
	_, e = activationPut(s, f.od, m, a, "disabled")
	monitorWant(t, e, session.ErrInvalid)
}
func TestActivationTriggerAtomicFailureAndBoundedAudit(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, fault := range []string{"missing", "overflow", "trigger"} {
		t.Run(fault, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			s := monitor.Store{Pool: db}
			a := activationRead(t, db, f.od, m)
			_, e := activationPut(s, f.od, m, a, "enabled")
			if e != nil {
				t.Fatal(e)
			}
			switch fault {
			case "missing":
				tx, _ := db.Begin(ctx)
				defer tx.Rollback(ctx)
				for _, q := range []string{`ALTER TABLE monitor_activation_states DISABLE TRIGGER monitor_activation_guard`, `DELETE FROM monitor_activation_states WHERE monitor_id='` + m.ID + `'`, `ALTER TABLE monitor_activation_states ENABLE TRIGGER monitor_activation_guard`} {
					if _, e = tx.Exec(ctx, q); e != nil {
						t.Fatal(e)
					}
				}
				if tx.Commit(ctx) != nil {
					t.Fatal("fixture")
				}
			case "overflow":
				activationSeed(t, db, m, "activation_generation=9223372036854775807")
				// This deliberately unreachable fixture must be removed before
				// schema8 Down retires all validators. Down must still reject
				// a real exhausted generation; do not weaken that protection.
				t.Cleanup(func() { activationSeed(t, db, m, "activation_generation=1") })
			case "trigger":
				if _, e = db.Exec(ctx, `CREATE FUNCTION activation_update_fixture_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER activation_update_fixture_fail BEFORE UPDATE ON monitor_activation_states FOR EACH ROW EXECUTE FUNCTION activation_update_fixture_fail()`); e != nil {
					t.Fatal(e)
				}
				defer db.Exec(ctx, `DROP TRIGGER activation_update_fixture_fail ON monitor_activation_states; DROP FUNCTION activation_update_fixture_fail()`)
			}
			_, e = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("new"), monitor.ETag(m))
			if e == nil {
				t.Fatal("partial config")
			}
			got, e := s.Detail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
			if e != nil || got != m {
				t.Fatal("config changed")
			}
			if fault == "overflow" {
				a = activationRead(t, db, f.od, m)
				_, e = activationPut(s, f.od, m, a, "disabled")
				monitorWant(t, e, user.ErrInternal)
			}
		})
	}
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	for i := 0; i < 64; i++ {
		var e error
		a, e = activationPut(s, f.od, m, a, "disabled")
		if e != nil {
			t.Fatal(e)
		}
	}
	if a.ActivationGeneration != "65" || queryInt(t, db, `SELECT count(*) FROM monitor_activation_states WHERE monitor_id=$1`, m.ID) != 1 {
		t.Fatal("unbounded audit")
	}
	t.Log(fmt.Sprintf("64 commands -> one row, generation %s; no run/lease/results", a.ActivationGeneration))
}
func TestActivationQuotaAndCAS(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	for i := 0; i < 99; i++ {
		n, e := s.Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput(fmt.Sprintf("quota-%d", i)))
		if e != nil {
			t.Fatal(e)
		}
		a := activationRead(t, db, f.od, n)
		if _, e = activationPut(s, f.od, n, a, "enabled"); e != nil {
			t.Fatal(e)
		}
	}
	n, e := s.Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("last"))
	if e != nil {
		t.Fatal(e)
	}
	pause := newWorkspacePause(t)
	first := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx {
		return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
			if stage == "gate" {
				pause.stop(ctx)
			}
		}}
	}}}
	a := startActivation(ctx, first, f.od, m, false)
	pause.wait(t)
	pid := make(chan int, 1)
	b := startActivation(ctx, monitor.Store{Pool: workspaceObservedDB{Pool: db, pid: pid}}, f.od, n, false)
	waitActivationBlock(t, db, <-pid)
	pause.resume()
	one := awaitActivation(t, a, nil)
	awaitActivation(t, b, monitor.ErrActivationLimit)
	if queryInt(t, db, `SELECT count(*) FROM monitor_activation_states WHERE workspace_id=$1 AND desired_state='enabled'`, f.w.ID) != 100 {
		t.Fatal("quota exceeded")
	}
	// Pause is not blocked by full quota or active cooling.
	one, e = activationPut(s, f.od, m, one, "disabled")
	if e != nil {
		t.Fatal(e)
	}
	_, e = activationPut(s, f.od, m, one, "enabled")
	var rate monitor.ActivationRateLimit
	if !errors.As(e, &rate) {
		t.Fatal("shared cooling", e)
	}
	two := activationRead(t, db, f.od, n)
	two, e = activationPut(s, f.od, n, two, "enabled")
	if e != nil {
		t.Fatal(e)
	}
	activationSeed(t, db, n, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	two = activationRead(t, db, f.od, n)
	if _, e = activationPut(s, f.od, n, two, "enabled"); e != nil {
		t.Fatal("same enabled at cap", e)
	}
	// A configuration change can revoke even while full and cooling.
	_, e = s.Update(ctx, f.od, n.WorkspaceID, n.ProjectID, n.ID, monitorInput("quota-patch"), monitor.ETag(n))
	if e != nil {
		t.Fatal(e)
	}
	t.Log("two Store instances serialized at Workspace: exactly100; 101st409; pause/PATCH allowed; cooling429")
}
func TestActivationCommitFaults(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, outcome := range []string{"rollback", "lost"} {
		t.Run(outcome, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			a := activationRead(t, db, f.od, m)
			var count atomic.Int32
			s := monitor.Store{Pool: workspaceObservedDB{Pool: db, wrap: func(tx pgx.Tx) pgx.Tx { return activationObservedTx{Tx: tx, outcome: outcome, commits: &count} }}}
			got, e := activationPut(s, f.od, m, a, "enabled")
			monitorWant(t, e, user.ErrUnavailable)
			if got.MonitorID != "" || count.Load() != 1 {
				t.Fatal("unconfirmed success or retry")
			}
			current := activationRead(t, db, f.od, m)
			want := "1"
			if outcome == "lost" {
				want = "2"
			}
			if current.ActivationGeneration != want {
				t.Fatal("commit outcome")
			}
			if outcome == "lost" {
				_, e = activationPut(monitor.Store{Pool: db}, f.od, m, a, "enabled")
				monitorWant(t, e, monitor.ErrPreconditionFailed)
			}
		})
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction'`) != 0 {
		t.Fatal("leaked transaction")
	}
	_ = ctx
}
func TestActivationWorkspaceIntentLifetime(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for _, kind := range []string{"owner-exits", "no-active-owner"} {
		t.Run(kind, func(t *testing.T) {
			f, m := monitorFixture(t, db)
			s := monitor.Store{Pool: db}
			a := activationRead(t, db, f.od, m)
			a, e := activationPut(s, f.od, m, a, "enabled")
			if e != nil {
				t.Fatal(e)
			}
			if kind == "owner-exits" {
				if _, e = db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); e != nil {
					t.Fatal(e)
				}
				if e = (workspace.Store{Pool: db}).DeleteMember(ctx, f.od, f.w.ID, f.owner.ID, f.condition(f.w.ID, f.owner.ID)); e != nil {
					t.Fatal(e)
				}
				_, e = activationPut(s, f.od, m, a, "disabled")
				monitorWant(t, e, monitor.ErrNotFound)
			} else {
				if _, e = db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, f.owner.ID); e != nil {
					t.Fatal(e)
				}
			}
			got := activationRead(t, db, f.md, m)
			if got.DesiredState != "enabled" || got.ActivationGeneration != a.ActivationGeneration || got.ExecutionState != "not_reported" || got.BlockedReason != "execution_status_required" || got.NextDueAt != nil {
				t.Fatal("intent/execution lifetime")
			}
		})
	}
}
