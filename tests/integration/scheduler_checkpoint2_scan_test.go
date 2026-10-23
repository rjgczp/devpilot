//go:build integration && schedulerfixture && linux

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

func TestCheckpointTwoBoundedScanAndTick(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	for i := range 65 {
		t.Run(fmt.Sprintf("workspace-%02d", i), func(t *testing.T) {
			f, m, _ := schedulerClaimFixtureSeconds(t, db, 3600)
			if i == 0 {
				for j := range 8 {
					next, err := (monitor.Store{Pool: db}).Create(ctx, f.od, m.WorkspaceID, m.ProjectID, monitorInput(fmt.Sprintf("extra%d", j)))
					if err != nil {
						t.Fatal(err)
					}
					a := activationRead(t, db, f.od, next)
					if _, err = (monitor.Store{Pool: db}).ExecutionAuthorize(ctx, f.od, next.WorkspaceID, next.ProjectID, next.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 3600}, monitor.ActivationETag(a)); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
	w, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	schedulerDatabaseOnlyGate(t, db)
	store := scheduler.Store{DB: w, Incarnation: inc}
	hints, err := store.Candidates(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, h := range hints {
		counts[h.WorkspaceID]++
		if counts[h.WorkspaceID] > 8 {
			t.Fatal("more than8 candidates perW")
		}
	}
	if len(counts) != 64 || len(hints) != 71 {
		t.Fatal("unbounded or incomplete scan", len(counts), len(hints))
	}
	last := hints[len(hints)-1].WorkspaceID
	rotated, err := store.Candidates(ctx, last)
	if err != nil || len(rotated) == 0 || rotated[0].WorkspaceID <= last {
		t.Fatal("cursor did not rotate to omitted W", err)
	}
	leases, cursor, err := store.Tick(ctx, "")
	if err != nil || len(leases) != 32 || cursor == "" {
		t.Fatal("tick attempt bound", len(leases), cursor, err)
	}
	unique := map[string]bool{}
	for _, l := range leases {
		if unique[l.WorkspaceID] {
			t.Fatal("more than one claim perW/tick")
		}
		unique[l.WorkspaceID] = true
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 32 || queryInt(t, db, `SELECT max(last_fencing_token) FROM monitor_schedules`) != 1 {
		t.Fatal("tick created excessive cycles")
	}
	t.Logf("real PG scan65W/73M ->64W/71hints <=8/W; tick32claims/32W; cursor=%s omittedW=%s", cursor, rotated[0].WorkspaceID)
}
