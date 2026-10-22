//go:build integration

// The CI-only fixture preparer deliberately refuses nonempty/schema8 databases.
// It is not an application migration command or a production entry point.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/rjgczp/devpilot/internal/testdb"
)

func prepare(ctx context.Context) error {
	db, err := testdb.Open(ctx, os.Getenv("TEST_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()
	var publicTables int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public'`).Scan(&publicTables); err != nil || publicTables != 0 {
		return fmt.Errorf("snapshot fixture requires its own empty guarded PG18 database")
	}
	version, err := exec.CommandContext(ctx, "goose", "-env=none", "-version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(version)) != "goose version: v3.26.0" {
		return fmt.Errorf("snapshot fixture requires pinned native Goose3.26.0")
	}
	cmd := exec.CommandContext(ctx, "goose", "-env=none", "-dir", "migrations", "-timeout", "30s", "up-to", "6")
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "GOOSE_") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "GOOSE_DRIVER=postgres", "GOOSE_DBSTRING="+os.Getenv("TEST_DATABASE_URL"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("pinned snapshot schema preparation failed")
	}
	var schema int
	var scheduler bool
	if err = db.QueryRow(ctx, `SELECT (SELECT max(version_id) FROM goose_db_version WHERE is_applied),to_regclass('public.monitor_schedules') IS NOT NULL`).Scan(&schema, &scheduler); err != nil || schema != 6 || scheduler {
		return fmt.Errorf("snapshot fixture schema6 isolation check failed")
	}
	fmt.Println("guarded PG18: native Goose3.26.0 up-to6; no schema8 Scheduler objects")
	return nil
}
func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if err := prepare(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
