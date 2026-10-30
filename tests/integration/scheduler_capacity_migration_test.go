//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchedulerCapacityMigrationLifecycle(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	for _, mode := range []string{"open-gate", "ledger-mismatch", "ddl-failure"} {
		t.Run(mode, func(t *testing.T) {
			goose(t, dir, "up-to 8", true)
			target := dir
			switch mode {
			case "open-gate":
				if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
					t.Fatal(e)
				}
			case "ledger-mismatch":
				if _, e := db.Exec(ctx, `UPDATE scheduler_budget_state SET terminal_count=1 WHERE kind='global'`); e != nil {
					t.Fatal(e)
				}
			case "ddl-failure":
				target = t.TempDir()
				files, e := os.ReadDir(dir)
				if e != nil {
					t.Fatal(e)
				}
				for _, f := range files {
					if !strings.HasSuffix(f.Name(), ".sql") {
						continue
					}
					b, e := os.ReadFile(filepath.Join(dir, f.Name()))
					if e != nil {
						t.Fatal(e)
					}
					if strings.HasPrefix(f.Name(), "00009_") {
						b = []byte(strings.Replace(string(b), "CREATE TRIGGER scheduler_run_capacity_insert", "SELECT 1/0;\nCREATE TRIGGER scheduler_run_capacity_insert", 1))
					}
					if e = os.WriteFile(filepath.Join(target, f.Name()), b, 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			out := goose(t, target, "up", false)
			t.Logf("original migration failure: %s", out)
			want := map[string]string{"open-gate": "closed fixture gate", "ledger-mismatch": "capacity ledger inconsistent", "ddl-failure": "division by zero"}[mode]
			if !strings.Contains(string(out), want) {
				t.Fatal("wrong failure boundary", string(out))
			}
			if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 8 || queryInt(t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name='scheduler_budget_state' AND column_name='retention_pending_count'`) != 0 || queryInt(t, db, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='scheduler_private' AND p.proname='run_capacity_accounting'`) != 0 {
				t.Fatal("migration failure was not atomic")
			}
			if !queryBoolCapacity(t, db, `SELECT has_table_privilege('devpilot_scheduler_api_owner','scheduler_budget_state','UPDATE')`) {
				t.Fatal("failed upgrade changed old ACL")
			}
			if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true; UPDATE scheduler_budget_state SET terminal_count=0 WHERE kind='global'`); e != nil {
				t.Fatal(e)
			}
			goose(t, dir, "up", true)
			goose(t, dir, "up", true)
			if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 9 {
				t.Fatal("schema9 missing")
			}
			goose(t, dir, "down", true)
			if !queryBoolCapacity(t, db, `SELECT has_table_privilege('devpilot_scheduler_api_owner','scheduler_budget_state','UPDATE')`) {
				t.Fatal("empty Down did not restore schema8 ACL")
			}
			goose(t, dir, "down-to 0", true)
		})
	}
}

func queryBoolCapacity(t *testing.T, db *pgxpool.Pool, sql string) bool {
	t.Helper()
	var b bool
	if e := db.QueryRow(context.Background(), sql).Scan(&b); e != nil {
		t.Fatal(e)
	}
	return b
}
