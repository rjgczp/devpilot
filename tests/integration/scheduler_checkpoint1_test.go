//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func checkpointReject(t *testing.T, err error, code string) {
	t.Helper()
	var p *pgconn.PgError
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("expected PostgreSQL %s, got %v", code, err)
	}
	t.Logf("database rejected SQLSTATE=%s constraint=%s", p.Code, p.ConstraintName)
}
func TestCheckpointOnePersistenceConstraints(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	if _, err := db.Exec(ctx, `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, m.WorkspaceID, m.ProjectID, m.ID); err != nil {
		t.Fatal(err)
	}
	reject := func(code, sql string, args ...any) {
		t.Helper()
		_, err := db.Exec(ctx, sql, args...)
		checkpointReject(t, err, code)
	}
	reject("23503", `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, f.z.ID, m.ProjectID, m.ID)
	var rid string
	const insertRun = `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state) SELECT $1,$2,$3,uuidv7(),1,1,1,$4,n,n,session_user,uuidv7(),n+interval '45 seconds','leased' FROM t RETURNING run_id::text`
	if err := db.QueryRow(ctx, insertRun, m.WorkspaceID, m.ProjectID, m.ID, 1).Scan(&rid); err != nil {
		t.Fatal(err)
	}
	reject("23505", insertRun, m.WorkspaceID, m.ProjectID, m.ID, 2)
	reject("23503", `INSERT INTO scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state) VALUES($1,$2,$3,$4,'reserved')`, f.z.ID, m.ProjectID, m.ID, rid)
	if _, err := db.Exec(ctx, `INSERT INTO scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state) VALUES($1,$2,$3,$4,'reserved')`, m.WorkspaceID, m.ProjectID, m.ID, rid); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`workspace_id=uuidv7()`, `project_id=uuidv7()`, `monitor_id=uuidv7()`, `run_id=uuidv7()`, `reservation_id=uuidv7()`, `reservation_state='released'`, `namespace_incarnation=uuidv7()`, `permit_deadline=clock_timestamp()+interval '1 hour'`, `origin_key='192.0.2.1'`, `ip_key=repeat('a',64)`} {
		reject("23514", `UPDATE scheduler_reservations SET `+set+` WHERE run_id=$1`, rid)
	}
	for _, set := range []string{`fencing_token=2`, `scheduler_epoch=2`, `configuration_version=2`, `activation_generation=2`, `lease_until=claimed_at`, `attempt_state='completed'`, `http_status=0`, `http_status=600`, `failure_code='raw_secret'`, `finished_at=claimed_at-interval '1 second'`} {
		reject("23514", `UPDATE monitor_runs SET `+set+` WHERE run_id=$1`, rid)
	}
	if _, err := db.Exec(ctx, `UPDATE monitor_schedules SET scheduler_epoch=2,last_fencing_token=1 WHERE monitor_id=$1`, m.ID); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`scheduler_epoch=1`, `last_fencing_token=0`, `workspace_id=uuidv7()`, `next_due_at=clock_timestamp()`} {
		reject("23514", `UPDATE monitor_schedules SET `+set+` WHERE monitor_id=$1`, m.ID)
	}
	reject("23514", `UPDATE scheduler_control SET gate='production' WHERE control_id='deployment'`)
	reject("23514", `UPDATE scheduler_budget_state SET in_use=33 WHERE kind='global'`)
	reject("23514", `UPDATE scheduler_budget_state SET tokens=8000001 WHERE kind='global'`)
	reject("23514", `INSERT INTO scheduler_budget_state(kind,budget_key,keep_until) VALUES('origin','https://private.example/path',clock_timestamp())`)
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='started',started_at=clock_timestamp() WHERE run_id=$1`, rid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='unknown',finished_at=clock_timestamp(),stop_reason='worker_lost' WHERE run_id=$1`, rid); err != nil {
		t.Fatal(err)
	}
	// Unknown is terminal but its physical slot is still outstanding.
	reject("23505", insertRun, m.WorkspaceID, m.ProjectID, m.ID, 2)
	// No Probe, input or namespace ever existed in this DB-only constraint fixture.
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, rid); err != nil {
		t.Fatal(err)
	}
	var second string
	if err := db.QueryRow(ctx, insertRun, m.WorkspaceID, m.ProjectID, m.ID, 2).Scan(&second); err != nil {
		t.Fatal("physical-release positive control", err)
	}
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='canceled',finished_at=clock_timestamp(),physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, second); err != nil {
		t.Fatal(err)
	}
	reject("23505", insertRun, m.WorkspaceID, m.ProjectID, m.ID, 1)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('monitor_schedules','monitor_runs','scheduler_reservations','scheduler_budget_state','scheduler_control') AND column_name ~ '(url|password|cookie|token_hash|header|body|cname|certificate)'`) != 0 {
		t.Fatal("sensitive persisted field")
	}
}

func TestCheckpointOneSchemaSevenFailsClosed(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "up-to 7", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	got, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	monitorWant(t, err, user.ErrInternal)
	if got.MonitorID != "" || monitor.ActivationETag(a) != monitor.ActivationETag(activationRead(t, db, f.od, m)) {
		t.Fatal("schema7 authorization partially succeeded")
	}
	if _, err = s.ExecutionStatus(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID); err == nil {
		t.Fatal("schema7 invented execution status")
	}
}

func TestCheckpointOneConfigurationRevokeRollback(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	grant, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `CREATE FUNCTION checkpoint1_revoke_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'checkpoint1 revocation failure'; END $$; CREATE TRIGGER checkpoint1_revoke_fail BEFORE UPDATE ON monitor_schedules FOR EACH ROW EXECUTE FUNCTION checkpoint1_revoke_fail()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP TRIGGER checkpoint1_revoke_fail ON monitor_schedules;DROP FUNCTION checkpoint1_revoke_fail()`); err != nil {
			t.Error(err)
		}
	})
	_, err = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("must roll back"), monitor.ETag(m))
	monitorWant(t, err, user.ErrInternal)
	after, err := s.Detail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil || monitor.ETag(after) != monitor.ETag(m) {
		t.Fatal("configuration partially changed", err)
	}
	if activationRead(t, db, f.od, m).ActivationGeneration != grant.ActivationGeneration || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND authorized_activation_generation=2 AND expires_at IS NOT NULL`, m.ID) != 1 {
		t.Fatal("grant partially revoked")
	}
}

func TestCheckpointOneRealExpiryExplicitRenewal(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	grant, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	if err != nil {
		t.Fatal(err)
	}
	if err = (session.Store{Pool: db}).Revoke(ctx, f.od); err != nil {
		t.Fatal(err)
	}
	before := activationRead(t, db, f.md, m)
	// Actual database time and the public minimum lifetime; no clock injection or
	// mutation of authorization/cooling timestamps. Every read is a short statement.
	for {
		var now time.Time
		if err = db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			t.Fatal(err)
		}
		if !now.Before(grant.ExpiresAt) {
			break
		}
		if now.After(grant.ExpiresAt.Add(3 * time.Second)) {
			t.Fatal("expiry observation overran")
		}
		time.Sleep(250 * time.Millisecond)
	}
	status, err := s.ExecutionStatus(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil || status.AuthorizationState != "expired" || status.EffectiveAuthorization || status.BlockedReason == nil || *status.BlockedReason != "authorization_expired" || status.ExpiresAt == nil || !status.ExpiresAt.Equal(grant.ExpiresAt) {
		t.Fatal("expiry renewed or hid grant", err, status)
	}
	after := activationRead(t, db, f.md, m)
	if monitor.ActivationETag(before) != monitor.ActivationETag(after) || !before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatal("expiry/logout changed intent")
	}
	if _, err = s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(after)); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("revoked session became a background identity", err)
	}
	if _, err = db.Exec(ctx, `UPDATE workspace_members SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, f.w.ID, f.member.ID); err != nil {
		t.Fatal(err)
	}
	renewed, err := s.ExecutionAuthorize(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 86400}, monitor.ActivationETag(after))
	if err != nil || renewed.ActivationGeneration != "3" || renewed.IssuedAt.Before(grant.ExpiresAt) || renewed.ExpiresAt.Sub(renewed.IssuedAt) != 24*time.Hour {
		t.Fatal("explicit Owner renewal", err)
	}
	t.Logf("database lifetime: issued=%s expired=%s explicit-renewal=%s", grant.IssuedAt, grant.ExpiresAt, renewed.IssuedAt)
}
