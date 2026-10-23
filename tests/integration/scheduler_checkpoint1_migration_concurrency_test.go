//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

type checkpointGooseResult struct {
	err    error
	output []byte
}

func checkpointStartGoose(t *testing.T, dir string) <-chan checkpointGooseResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, "goose", "-env=none", "-dir", dir, "-timeout", "30s", "up-to", "8")
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GOOSE_") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Env = append(cmd.Env, "GOOSE_DRIVER=postgres", "GOOSE_DBSTRING="+os.Getenv("TEST_DATABASE_URL"))
	ch := make(chan checkpointGooseResult, 1)
	go func() { out, err := cmd.CombinedOutput(); ch <- checkpointGooseResult{err, out} }()
	return ch
}
func checkpointAwaitGoose(t *testing.T, ch <-chan checkpointGooseResult) {
	t.Helper()
	r := <-ch
	if r.err != nil {
		t.Fatalf("migration failed; diagnostic bytes=%d", len(r.output))
	}
	t.Log("native Goose 7->8 confirmed commit")
}
func checkpointMigrationWait(t *testing.T, db *pgxpool.Pool, advisory bool) int {
	t.Helper()
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		var pid int
		sql := `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active' AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE public.monitor_activation_states%' LIMIT 1`
		if advisory {
			sql = `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active' AND wait_event='advisory' LIMIT 1`
		}
		if db.QueryRow(context.Background(), sql).Scan(&pid) == nil {
			return pid
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("migration barrier not observed within existing API budget")
	return 0
}
func TestCheckpointOneOldWritersStraddleMigration(t *testing.T) {
	for _, order := range []string{"activation-first", "patch-first", "migration-first"} {
		t.Run(order, func(t *testing.T) {
			db := openDB(t)
			ctx := context.Background()
			dir := filepath.Join(root(), "migrations")
			goose(t, dir, "up-to 7", true)
			t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
			f, m := monitorFixture(t, db)
			if _, err := db.Exec(ctx, `CREATE ROLE checkpoint1_legacy LOGIN PASSWORD 'disposable_fixture_only'; GRANT USAGE ON SCHEMA public TO checkpoint1_legacy;GRANT SELECT,UPDATE ON users,sessions,workspaces,workspace_members,projects,http_monitors,monitor_activation_states TO checkpoint1_legacy`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := db.Exec(ctx, `DROP OWNED BY checkpoint1_legacy;DROP ROLE checkpoint1_legacy`); err != nil {
					t.Error(err)
				}
			})
			if _, err := db.Exec(ctx, legacyPatchInsertColumns); err != nil {
				t.Fatal(err)
			}
			cfg := db.Config()
			cfg.ConnConfig.User = "checkpoint1_legacy"
			cfg.ConnConfig.Password = "disposable_fixture_only"
			oldPool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(oldPool.Close)
			old := monitor.SchemaSevenFixtureStore{Pool: oldPool}
			a, err := old.ActivationDetail(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID)
			if err != nil {
				t.Fatal(err)
			}
			pause := newWorkspacePause(t)
			pid := make(chan int, 1)
			done := make(chan error, 1)
			if order == "migration-first" {
				blocker, err := db.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer blocker.Rollback(ctx)
				if _, err = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(181108)`); err != nil {
					t.Fatal(err)
				}
				temp := t.TempDir()
				files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
				if err != nil {
					t.Fatal(err)
				}
				for _, file := range files {
					raw, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(filepath.Base(file), "00008_") {
						raw = []byte(strings.Replace(string(raw), "-- +goose Down", "SELECT pg_advisory_xact_lock(181108);\n-- +goose Down", 1))
					}
					if err = os.WriteFile(filepath.Join(temp, filepath.Base(file)), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				migration := checkpointStartGoose(t, temp)
				mp := checkpointMigrationWait(t, db, true)
				go func() {
					_, e := (monitor.SchemaSevenFixtureStore{Pool: workspaceObservedDB{Pool: oldPool, pid: pid}}).ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "enabled", ConfigurationVersion: "1"}, monitor.ActivationETag(a))
					done <- e
				}()
				actor := <-pid
				waitActivationBlock(t, db, actor)
				schedulerLockReceipt(t, db, "Migration holds table locks; old API waits Activation", mp, actor)
				if err = blocker.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				checkpointAwaitGoose(t, migration)
				monitorWant(t, <-done, monitor.ErrPreconditionFailed)
				if activationRead(t, db, f.od, m).ActivationGeneration != "2" {
					t.Fatal("migration-first reused tag")
				}
			} else {
				observed := workspaceObservedDB{Pool: oldPool, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
					if order == "activation-first" {
						return activationObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
							if stage == "activation" {
								pause.stop(ctx)
							}
						}}
					}
					return monitorObservedTx{Tx: tx, after: func(ctx context.Context, stage string) {
						if stage == "monitor" {
							pause.stop(ctx)
						}
					}}
				}}
				go func() {
					var e error
					if order == "activation-first" {
						_, e = (monitor.SchemaSevenFixtureStore{Pool: observed}).ActivationUpdate(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ActivationInput{DesiredState: "enabled", ConfigurationVersion: "1"}, monitor.ActivationETag(a))
					} else {
						_, e = (monitor.Store{Pool: observed}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitorInput("old PATCH after schema8"), monitor.ETag(m))
					}
					done <- e
				}()
				actor := <-pid
				pause.wait(t)
				migration := checkpointStartGoose(t, dir)
				if order == "activation-first" {
					mp := checkpointMigrationWait(t, db, false)
					schedulerLockReceipt(t, db, "old Activation writer precedes Migration retirement", actor, mp)
					pause.resume()
					monitorWant(t, <-done, nil)
					checkpointAwaitGoose(t, migration)
				} else {
					checkpointAwaitGoose(t, migration)
					schedulerLockReceipt(t, db, "Migration commits before old PATCH DML; no child-to-parent cycle", actor)
					pause.resume()
					monitorWant(t, <-done, nil)
				}
				after := activationRead(t, db, f.od, m)
				if after.ActivationGeneration != "3" || after.DesiredState != "disabled" {
					t.Fatal("old writer/migration generation order", after.ActivationGeneration)
				}
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_schedules`)+queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
				t.Fatal("straddling writer granted execution")
			}
		})
	}
}
