//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/user"
)

func TestSchedulerLegacyDatabaseWriterAcrossMigration(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 7", true)
	f, m := monitorFixture(t, db)
	if _, err := db.Exec(ctx, `CREATE ROLE scheduler_test_legacy LOGIN PASSWORD 'disposable_fixture_only'; GRANT USAGE ON SCHEMA public TO scheduler_test_legacy; GRANT SELECT,UPDATE ON users,sessions,workspaces,workspace_members,projects,http_monitors,monitor_activation_states TO scheduler_test_legacy`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP OWNED BY scheduler_test_legacy;DROP ROLE scheduler_test_legacy`); err != nil {
			t.Error(err)
		}
	})
	cfg := db.Config()
	cfg.ConnConfig.User = "scheduler_test_legacy"
	cfg.ConnConfig.Password = "disposable_fixture_only"
	oldPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(oldPool.Close)
	old := monitor.SchemaSevenFixtureStore{Pool: oldPool}
	before, err := old.ActivationDetail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.ExecutionState != "blocked" || before.BlockedReason != "scheduler_unavailable" {
		t.Fatal("historical writer was altered")
	}
	enabled, err := activationPut(old, f.od, m, before, "enabled")
	if err != nil {
		t.Fatal(err)
	}
	goose(t, dir, "up-to 8", true)
	// The same pre-upgrade LOGIN and pool remain connected, but are NEVER routed
	// as an HTTP server. Every old tag is retired and no new object is writable.
	if _, err = activationPut(old, f.od, m, enabled, "enabled"); err != monitor.ErrPreconditionFailed {
		t.Fatal("old tag survived", err)
	}
	for _, sql := range []string{`SELECT * FROM monitor_schedules`, `UPDATE monitor_schedules SET expires_at=clock_timestamp()+interval '1 day'`, `SELECT * FROM scheduler_private.authorize(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,60)`} {
		if _, err = oldPool.Exec(ctx, sql); err == nil {
			t.Fatal("legacy role gained capability", sql)
		}
	}
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	a, err := old.ActivationDetail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err = activationPut(old, f.od, m, a, "enabled")
	if err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1`, m.ID) != 0 {
		t.Fatal("legacy enabled created schedule")
	}
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	s := monitor.Store{Pool: db}
	grant, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	if err != nil {
		t.Fatal(err)
	}
	a = activationRead(t, db, f.od, m)
	if a.ActivationGeneration != grant.ActivationGeneration {
		t.Fatal("new grant not bound")
	}
	activationSeed(t, db, m, "next_enable_allowed_at=statement_timestamp()-interval '1s'")
	if _, err = activationPut(old, f.od, m, a, "enabled"); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation IS NULL AND expires_at IS NULL AND next_due_at IS NULL`, m.ID) != 1 {
		t.Fatal("old writer failed to revoke grant")
	}
}

func TestExecutionAuthorizationScheduleFailureRollsBackIntent(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	a := activationRead(t, db, f.od, m)
	if _, err := db.Exec(ctx, `CREATE FUNCTION scheduler_test_grant_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture grant failure'; END $$; CREATE TRIGGER scheduler_test_grant_fail BEFORE INSERT ON monitor_schedules FOR EACH ROW EXECUTE FUNCTION scheduler_test_grant_fail()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP TRIGGER scheduler_test_grant_fail ON monitor_schedules;DROP FUNCTION scheduler_test_grant_fail()`); err != nil {
			t.Error(err)
		}
	})
	_, err := (monitor.Store{Pool: db}).ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	monitorWant(t, err, user.ErrInternal)
	after := activationRead(t, db, f.od, m)
	if monitor.ActivationETag(a) != monitor.ActivationETag(after) || after.DesiredState != "disabled" || after.NextEnableAllowedAt != nil || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1`, m.ID) != 0 {
		t.Fatal("partial intent/grant or cooling survived rollback")
	}
}
