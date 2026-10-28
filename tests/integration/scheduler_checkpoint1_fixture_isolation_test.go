//go:build integration

package integration

import (
	"context"
	"testing"
)

// Composite acceptance catches state left between independent suite cases,
// which isolated executions cannot expose. Keep the real authorization test.
func TestCheckpointOneAuthorizationFixtureCleansSharedDatabase(t *testing.T) {
	db := openDB(t)
	t.Cleanup(func() { goose(t, root()+"/migrations", "down-to 0", true) })
	t.Run("authorization", TestExecutionAuthorizationAtomicIntentAndRevocation)
	var version, schedules int
	var exists bool
	if err := db.QueryRow(context.Background(), `SELECT coalesce(max(version_id),0) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(context.Background(), `SELECT to_regclass('public.monitor_schedules') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		schedules = queryInt(t, db, `SELECT count(*) FROM monitor_schedules`)
	}
	t.Logf("post-test isolation: migration=%d scheduler_table=%v schedule_rows=%d", version, exists, schedules)
	if version != 0 || exists {
		t.Fatal("authorization fixture leaked state into following capacity test")
	}
}
