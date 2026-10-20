//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/rjgczp/devpilot/internal/monitor"
)

// These rows are constraint/projection fixtures only. This test never creates a
// namespace, sends an envelope or starts a Probe. It is not physical-stop proof.
func TestSchedulerPersistenceScopeLineageAndShape(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	a := activationRead(t, db, f.od, m)
	if _, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a)); err != nil {
		t.Fatal(err)
	}
	reject := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err == nil {
			t.Fatal("accepted invalid persistence", sql)
		}
	}
	reject(`INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) VALUES($1,$2,$3)`, f.z.ID, m.ProjectID, m.ID)
	for _, set := range []string{`workspace_id=uuidv7()`, `project_id=uuidv7()`, `monitor_id=uuidv7()`, `created_at=created_at+interval '1 second'`, `scheduler_epoch=0`, `scheduler_epoch=scheduler_epoch-1`, `last_fencing_token=-1`, `expires_at=NULL`, `consent_protocol=NULL`, `expires_at=issued_at+interval '24 hours 1 second'`, `authorized_activation_generation=NULL`, `authorized_configuration_version=0`} {
		reject(`UPDATE monitor_schedules SET `+set+` WHERE monitor_id=$1`, m.ID)
	}
	var runID string
	if err := db.QueryRow(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state) SELECT $1,$2,$3,uuidv7(),1,2,1,1,n,n,session_user,uuidv7(),n+interval '45 seconds','leased' FROM t RETURNING run_id::text`, m.WorkspaceID, m.ProjectID, m.ID).Scan(&runID); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`workspace_id=uuidv7()`, `project_id=uuidv7()`, `monitor_id=uuidv7()`, `run_id=uuidv7()`, `configuration_version=2`, `activation_generation=3`, `scheduler_epoch=2`, `fencing_token=2`, `scheduled_at=scheduled_at+interval '1ms'`, `claimed_at=claimed_at+interval '1ms'`, `creator_transaction=pg_current_xact_id()`, `worker_incarnation=uuidv7()`, `lease_owner='forged'`, `lease_until=claimed_at`, `attempt_state='running'`, `attempt_state='completed'`, `attempt_state='unknown'`, `http_status=0`, `success=false`, `failure_code='tls-private-error'`, `stop_reason='private-error'`} {
		reject(`UPDATE monitor_runs SET `+set+` WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4`, m.WorkspaceID, m.ProjectID, m.ID, runID)
	}
	reject(`INSERT INTO monitor_runs SELECT workspace_id,project_id,monitor_id,uuidv7(),configuration_version,activation_generation,scheduler_epoch,fencing_token+1,scheduled_at,claimed_at,started_at,finished_at,pg_current_xact_id(),lease_owner,worker_incarnation,lease_until,attempt_state,result_commit_id,duration_ms,http_status,success,failure_code,physical_release_confirmed_at,stop_reason FROM monitor_runs WHERE run_id=$1`, runID)
	reject(`INSERT INTO scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state) VALUES($1,$2,$3,$4,'reserved')`, f.z.ID, m.ProjectID, m.ID, runID)
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='started',started_at=clock_timestamp() WHERE run_id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	reject(`UPDATE monitor_runs SET attempt_state='leased',started_at=NULL WHERE run_id=$1`, runID)
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET attempt_state='unknown',finished_at=clock_timestamp(),stop_reason='worker_lost' WHERE run_id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	for _, set := range []string{`attempt_state='started',finished_at=NULL`, `lease_until=lease_until+interval '1 second'`, `http_status=200,success=true`, `stop_reason='lease_expired'`} {
		reject(`UPDATE monitor_runs SET `+set+` WHERE run_id=$1`, runID)
	}
	// Administrator-only fixture teardown: no physical input ever existed. The
	// runtime worker cannot write this marker or call a supervisor-only function.
	if _, err := db.Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, runID); err != nil {
		t.Fatal(err)
	}
	var sensitive int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND table_name IN ('monitor_runs','monitor_schedules','scheduler_budget_state','scheduler_reservations','scheduler_control') AND (column_name ~ '(url|password|token_hash|cookie|header|body|cname|address|certificate)' OR column_name IN ('ip','dns','hmac_key'))`).Scan(&sensitive); err != nil || sensitive != 0 {
		t.Fatal("sensitive persistence field", err, sensitive)
	}
}
