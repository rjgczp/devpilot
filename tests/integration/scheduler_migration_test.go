//go:build integration

package integration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/project"
)

// Every destructive lifecycle begins with openDB: database name, PG18 and the
// separation from DATABASE_URL are checked before even invoking Goose.
func TestSchedulerMigrationRetiresAllLegacyTags(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 7", true)
	f, disabled := monitorFixture(t, db)
	enabled, err := (monitor.Store{Pool: db}).Create(ctx, f.od, disabled.WorkspaceID, disabled.ProjectID, monitorInput("enabled"))
	if err != nil {
		t.Fatal(err)
	}
	p2, e := (project.Store{Pool: db}).Create(ctx, f.xd, f.z.ID, projectInput("second tenant"))
	if e != nil {
		t.Fatal(e)
	}
	second, e := (monitor.Store{Pool: db}).Create(ctx, f.xd, f.z.ID, p2.ID, monitorInput("second disabled"))
	if e != nil {
		t.Fatal(e)
	}
	a := activationRead(t, db, f.od, enabled)
	if _, err = activationPut(monitor.Store{Pool: db}, f.od, enabled, a, "enabled"); err != nil {
		t.Fatal(err)
	}
	var before string
	if err = db.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(m.id,to_jsonb(m),a.activation_generation,a.next_enable_allowed_at) ORDER BY m.id)::text FROM http_monitors m JOIN monitor_activation_states a ON (a.workspace_id,a.project_id,a.monitor_id)=(m.workspace_id,m.project_id,m.id)`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	goose(t, dir, "up-to 8", true)
	for _, m := range []monitor.Public{disabled, enabled, second} {
		digest := f.od
		if m.ID == second.ID {
			digest = f.xd
		}
		a = activationRead(t, db, digest, m)
		want := "2"
		if m.ID == enabled.ID {
			want = "3"
		}
		if a.ActivationGeneration != want || a.DesiredState != "disabled" || a.ActivationConfigurationVersion != nil || a.LastTransitionKind != "upgrade_revoked" || a.NextDueAt != nil {
			t.Fatal("unsafe retirement", a)
		}
	}
	var after string
	if err = db.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(m.id,to_jsonb(m),a.activation_generation-1,a.next_enable_allowed_at) ORDER BY m.id)::text FROM http_monitors m JOIN monitor_activation_states a ON (a.workspace_id,a.project_id,a.monitor_id)=(m.workspace_id,m.project_id,m.id)`).Scan(&after); err != nil || before != after {
		t.Fatal("configuration or cooling changed", err)
	}
	for _, table := range []string{"monitor_schedules", "monitor_runs", "scheduler_reservations"} {
		if queryInt(t, db, "SELECT count(*) FROM "+table) != 0 {
			t.Fatal("upgrade granted work", table)
		}
	}
	goose(t, dir, "up-to 8", true) // Repeat the historical migration8, not migration9.
	if activationRead(t, db, f.od, enabled).ActivationGeneration != "3" {
		t.Fatal("repeat up consumed generation")
	}
	var beforeDown, afterDown string
	if err = db.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(monitor_id,activation_generation,next_enable_allowed_at) ORDER BY monitor_id)::text FROM monitor_activation_states`).Scan(&beforeDown); err != nil {
		t.Fatal(err)
	}
	// No gate or runs exist; disposable down cannot revive intent or old tags.
	goose(t, dir, "down", true)
	if err = db.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_array(monitor_id,activation_generation-1,next_enable_allowed_at) ORDER BY monitor_id)::text FROM monitor_activation_states`).Scan(&afterDown); err != nil || beforeDown != afterDown {
		t.Fatal("down changed cooling or failed to retire every tag", err)
	}
	a = activationRead(t, db, f.od, enabled)
	if a.DesiredState != "disabled" || a.ActivationGeneration != "4" || a.NextEnableAllowedAt == nil {
		t.Fatal("unsafe down")
	}
	goose(t, dir, "up-to 8", true) // Re-up the same historical representation protocol.
	if activationRead(t, db, f.od, enabled).ActivationGeneration != "5" {
		t.Fatal("re-up failed to retire representation")
	}
}

func TestSchedulerMigrationRollback(t *testing.T) {
	db := openDB(t)
	dir := filepath.Join(root(), "migrations")
	ctx := context.Background()
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	for _, mode := range []string{"disabled-overflow", "enabled-overflow", "trigger-failure", "mid-ddl-failure", "late-ddl-failure"} {
		t.Run(mode, func(t *testing.T) {
			goose(t, dir, "up-to 7", true)
			f, m := monitorFixture(t, db)
			if mode == "enabled-overflow" {
				a := activationRead(t, db, f.od, m)
				if _, e := activationPut(monitor.Store{Pool: db}, f.od, m, a, "enabled"); e != nil {
					t.Fatal(e)
				}
			}
			if strings.Contains(mode, "overflow") {
				activationSeed(t, db, m, "activation_generation=9223372036854775807")
			}
			if mode == "trigger-failure" {
				if _, e := db.Exec(ctx, `CREATE FUNCTION scheduler_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$; CREATE TRIGGER zz_scheduler_test_fail BEFORE UPDATE ON monitor_activation_states FOR EACH ROW EXECUTE FUNCTION scheduler_test_fail()`); e != nil {
					t.Fatal(e)
				}
			}
			var before, after string
			if e := db.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY workspace_id,project_id,monitor_id)::text FROM monitor_activation_states a`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			use := dir
			if strings.Contains(mode, "ddl-failure") {
				use = t.TempDir()
				files, e := filepath.Glob(filepath.Join(dir, "*.sql"))
				if e != nil {
					t.Fatal(e)
				}
				for _, p := range files {
					b, e := os.ReadFile(p)
					if e != nil {
						t.Fatal(e)
					}
					if strings.HasPrefix(filepath.Base(p), "00008_") {
						if mode == "mid-ddl-failure" {
							b = []byte(strings.Replace(string(b), "CREATE TABLE public.monitor_runs (", "CREATE TABLE scheduler_test_abort (n integer); SELECT 1/0;\nCREATE TABLE public.monitor_runs (", 1))
						} else {
							b = []byte(strings.Replace(string(b), "-- +goose Down", "CREATE TABLE scheduler_test_abort (n integer); SELECT 1/0;\n-- +goose Down", 1))
						}
					}
					if e = os.WriteFile(filepath.Join(use, filepath.Base(p)), b, 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			diagnostic := goose(t, use, "up", false)
			expected := "bigint out of range"
			if mode == "trigger-failure" {
				expected = "injected"
			}
			if strings.Contains(mode, "ddl-failure") {
				expected = "division by zero"
			}
			if !strings.Contains(string(diagnostic), expected) {
				t.Fatal("migration failed before intended fault boundary")
			}
			if e := db.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(a) ORDER BY workspace_id,project_id,monitor_id)::text FROM monitor_activation_states a`).Scan(&after); e != nil || before != after {
				t.Fatal("partial tag retirement", e)
			}
			if queryInt(t, db, `SELECT count(*) FROM pg_class WHERE relname IN ('monitor_schedules','monitor_runs','scheduler_test_abort')`)+queryInt(t, db, `SELECT count(*) FROM pg_roles WHERE rolname LIKE 'devpilot_scheduler_%'`)+queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=8 AND is_applied`) != 0 {
				t.Fatal("partial DDL/role/version receipt")
			}
			if mode == "trigger-failure" {
				if _, e := db.Exec(ctx, `DROP TRIGGER zz_scheduler_test_fail ON monitor_activation_states;DROP FUNCTION scheduler_test_fail()`); e != nil {
					t.Fatal(e)
				}
			}
			goose(t, dir, "down-to 0", true)
		})
	}
}

func TestSchemaSevenFixtureMatchesReviewedHistoricalWriter(t *testing.T) {
	handler, err := os.ReadFile(filepath.Join(root(), "internal/monitor/activation_handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	historicalHandler := strings.Replace(string(handler), "\tw.Header().Set(\"DevPilot-Activation-Projection\", \"intent-v2\")\n", "", 1)
	if fmt.Sprintf("%x", sha256.Sum256([]byte(historicalHandler))) != "7b0ed0c6848f079a05556a53ee92dd29950f133402f8397fa1020b7d1674aab6" {
		t.Fatal("historical HTTP fixture assumption changed")
	}
	raw, err := os.ReadFile(filepath.Join(root(), "internal/monitor/schema7_fixture_integration.go"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	source = source[strings.Index(source, "package monitor\n"):]
	source = strings.ReplaceAll(source, "// SchemaSevenFixtureStore is a frozen historical DB writer for schema7 and\n// cross-Migration8 tests. It is excluded from ordinary production builds.\ntype SchemaSevenFixtureStore Store\n\n", "")
	for _, pair := range [][2]string{{"func (s SchemaSevenFixtureStore)", "func (s Store)"}, {"schemaSevenActivationJSON", "activationJSON"}, {"schemaSevenDecodeActivation", "decodeActivation"}, {"schemaSevenActivationWriteSQL", "activationWriteSQL"}} {
		source = strings.ReplaceAll(source, pair[0], pair[1])
	}
	if fmt.Sprintf("%x", sha256.Sum256([]byte(source))) != "4fe9c60fcdb11901ba51b20af25e2a3682a0c54d4f00b87b883181ea79b7d591" {
		t.Fatal("schema7 fixture no longer matches historical code")
	}
}
