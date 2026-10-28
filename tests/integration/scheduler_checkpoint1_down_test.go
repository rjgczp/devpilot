//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckpointOneDownRefusesUnsafeOrExhausted(t *testing.T) {
	for _, mode := range []string{"open-gate", "unreleased-run", "disabled-overflow", "enabled-overflow"} {
		t.Run(mode, func(t *testing.T) {
			db := workspaceDB(t)
			ctx := context.Background()
			f, m, h := schedulerClaimFixture(t, db)
			switch mode {
			case "open-gate":
				if _, err := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); err != nil {
					t.Fatal(err)
				}
			case "unreleased-run":
				if _, err := db.Exec(ctx, `WITH t AS MATERIALIZED(SELECT clock_timestamp() n) INSERT INTO monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state) SELECT $1,$2,$3,$4,$5,$6,$7,1,n,n,session_user,uuidv7(),n+interval '45 seconds','leased' FROM t`, h.w, h.p, h.m, h.r, h.v, h.g, h.e); err != nil {
					t.Fatal(err)
				}
			case "disabled-overflow":
				activationSeed(t, db, m, "desired_state='disabled',activation_configuration_version=NULL,last_transition_kind='configuration_changed',last_actor_user_id=NULL,activation_generation=9223372036854775807")
			case "enabled-overflow":
				activationSeed(t, db, m, "activation_generation=9223372036854775807")
			}
			t.Cleanup(func() {
				// Only database projection fixtures, never a physical stop receipt.
				if _, err := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'; UPDATE monitor_runs SET attempt_state='canceled',finished_at=clock_timestamp(),physical_release_confirmed_at=clock_timestamp() WHERE physical_release_confirmed_at IS NULL`); err != nil {
					t.Error(err)
				}
				if strings.Contains(mode, "overflow") {
					activationSeed(t, db, m, "desired_state='disabled',activation_configuration_version=NULL,last_transition_kind='configuration_changed',last_actor_user_id=NULL,activation_generation=2")
				}
			})
			var before, after string
			const snapshot = `SELECT jsonb_build_object('activation',(SELECT jsonb_agg(to_jsonb(a) ORDER BY monitor_id) FROM monitor_activation_states a),'schedules',(SELECT jsonb_agg(to_jsonb(s) ORDER BY monitor_id) FROM monitor_schedules s),'runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY run_id) FROM monitor_runs r),'roles',(SELECT jsonb_agg(rolname ORDER BY rolname) FROM pg_roles WHERE rolname LIKE 'devpilot_scheduler_%'))::text`
			if err := db.QueryRow(ctx, snapshot).Scan(&before); err != nil {
				t.Fatal(err)
			}
			out := goose(t, filepath.Join(root(), "migrations"), "down", false)
			want := "independent physical stop and closed gate required"
			if strings.Contains(mode, "overflow") {
				want = "bigint out of range"
			}
			if !strings.Contains(string(out), want) {
				t.Fatal("Down failed before intended boundary")
			}
			t.Logf("expected Down failure %s: %s", mode, out)
			if err := db.QueryRow(ctx, snapshot).Scan(&after); err != nil || after != before {
				t.Fatal("Down left partial tag/schema/permission changes", err)
			}
			if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 8 {
				t.Fatal("failed Down changed migration version")
			}
			_ = f
		})
	}
}
