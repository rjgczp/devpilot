//go:build integration

package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/rjgczp/devpilot/internal/monitor"
)

func TestCheckpointOneRegistrationCapacityLastSlot(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	second, err := s.Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput("second"))
	if err != nil {
		t.Fatal(err)
	}
	// Real PG rows and composite FKs, not a mocked/counter-only capacity state.
	// Administrator fixture loading grants no execution authorization.
	if _, err = db.Exec(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) SELECT $1,$2,'capacity-fixture','https://example.com/',$3 FROM generate_series(1,9999)`, m.WorkspaceID, m.ProjectID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO monitor_schedules(workspace_id,project_id,monitor_id) SELECT workspace_id,project_id,id FROM http_monitors WHERE workspace_id=$1 AND name='capacity-fixture'`, m.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_schedules`) != 9999 || queryInt(t, db, `SELECT schedule_count FROM scheduler_budget_state WHERE kind='global'`) != 9999 {
		t.Fatal("real registration count diverged")
	}
	results := make(chan struct {
		m   monitor.Public
		err error
	}, 2)
	for _, candidate := range []monitor.Public{m, second} {
		a := activationRead(t, db, f.od, candidate)
		go func(v monitor.Public, a monitor.Activation) {
			_, e := s.ExecutionAuthorize(ctx, f.od, v.WorkspaceID, v.ProjectID, v.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
			results <- struct {
				m   monitor.Public
				err error
			}{v, e}
		}(candidate, a)
	}
	var winner, loser monitor.Public
	for range 2 {
		r := <-results
		if r.err == nil {
			if winner.ID != "" {
				t.Fatal("last slot granted twice")
			}
			winner = r.m
		} else if errors.Is(r.err, monitor.ErrRetentionCapacity) {
			loser = r.m
		} else {
			t.Fatal("registration conflict", r.err)
		}
	}
	if winner.ID == "" || loser.ID == "" || queryInt(t, db, `SELECT count(*) FROM monitor_schedules`) != 10000 || queryInt(t, db, `SELECT schedule_count FROM scheduler_budget_state WHERE kind='global'`) != 10000 {
		t.Fatal("registration cap is not atomic")
	}
	if activationRead(t, db, f.od, loser).ActivationGeneration != "1" {
		t.Fatal("capacity rejection changed intent")
	}
	a := activationRead(t, db, f.od, winner)
	if _, err = activationPut(s, f.od, winner, a, "disabled"); err != nil {
		t.Fatal("capacity blocked pause", err)
	}
	if _, err = s.Update(ctx, f.od, loser.WorkspaceID, loser.ProjectID, loser.ID, monitorInput("patch at capacity"), monitor.ETag(loser)); err != nil {
		t.Fatal("capacity blocked configuration PATCH", err)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
		t.Fatal("registration created runnable work")
	}
	t.Log("9999 real schedules + two competing grants -> 10000 schedules, one accepted, one retention_capacity; pause/PATCH remain available")
}
func TestCheckpointOneEnabledQuotaAndCooling(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	s := monitor.Store{Pool: db}
	if _, err := db.Exec(ctx, `INSERT INTO http_monitors(workspace_id,project_id,name,url,created_by_user_id) SELECT $1,$2,'quota-fixture','https://example.com/',$3 FROM generate_series(1,100)`, m.WorkspaceID, m.ProjectID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE monitor_activation_states a SET desired_state='enabled',activation_configuration_version=1,activation_generation=activation_generation+1,next_enable_allowed_at=clock_timestamp()+interval '60 seconds',last_transition_kind='owner_enabled',last_actor_user_id=$2,updated_at=clock_timestamp() FROM http_monitors m WHERE m.workspace_id=$1 AND m.name='quota-fixture' AND (a.workspace_id,a.project_id,a.monitor_id)=(m.workspace_id,m.project_id,m.id)`, m.WorkspaceID, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	a := activationRead(t, db, f.od, m)
	input := monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}
	_, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a))
	monitorWant(t, err, monitor.ErrActivationLimit)
	if activationRead(t, db, f.od, m).ActivationGeneration != "1" || queryInt(t, db, `SELECT count(*) FROM monitor_schedules`) != 0 {
		t.Fatal("quota failure partially granted")
	}
	var free monitor.Public
	if err = db.QueryRow(ctx, `SELECT id::text,workspace_id::text,project_id::text FROM http_monitors WHERE workspace_id=$1 AND name='quota-fixture' ORDER BY id LIMIT 1`, m.WorkspaceID).Scan(&free.ID, &free.WorkspaceID, &free.ProjectID); err != nil {
		t.Fatal(err)
	}
	if _, err = activationPut(s, f.od, free, activationRead(t, db, f.od, free), "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a)); err != nil {
		t.Fatal(err)
	}
	a = activationRead(t, db, f.od, m)
	if a.NextEnableAllowedAt == nil {
		t.Fatal("missing persistent cooling")
	}
	cooling := *a.NextEnableAllowedAt
	if _, err = activationPut(s, f.od, m, a, "disabled"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("new version during cooling"), monitor.ETag(m)); err != nil {
		t.Fatal(err)
	}
	a = activationRead(t, db, f.od, m)
	if a.NextEnableAllowedAt == nil || !a.NextEnableAllowedAt.Equal(cooling) {
		t.Fatal("pause/PATCH shortened cooling")
	}
	input.ConfigurationVersion = "2"
	_, err = s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, input, monitor.ActivationETag(a))
	var limit monitor.ActivationRateLimit
	if !errors.As(err, &limit) || limit.RetryAfter < 1 || queryInt(t, db, `SELECT count(*) FROM monitor_schedules WHERE monitor_id=$1 AND expires_at IS NOT NULL`, m.ID) != 0 {
		t.Fatal("new version reset cooling or restored permission", err)
	}
}
