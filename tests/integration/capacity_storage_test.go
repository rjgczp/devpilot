//go:build integration && schedulerfixture && privateworker && linux && budgetfixture && capacityfixture

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func capacitySchema(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var cluster string
	if db.QueryRow(ctx, `SHOW cluster_name`).Scan(&cluster) != nil || cluster != "devpilot-budget-disposable" {
		t.Fatal("disposable PG18 only")
	}
	goose(t, filepath.Join(root(), "migrations"), "up", true)
	approvalRaw, e := os.ReadFile(filepath.Join(root(), "deploy/capacity/isolated-test-approval.json"))
	if e != nil {
		t.Fatal(e)
	}
	var approval struct {
		Candidate  string `json:"candidate_sha256"`
		Private    string `json:"private_mode_sha256"`
		Production bool   `json:"production"`
	}
	if json.Unmarshal(approvalRaw, &approval) != nil || approval.Production {
		t.Fatal("no isolated approval")
	}
	for _, candidate := range []struct{ path, hash string }{{"deploy/privateloop/private-mode.sql.candidate", approval.Private}, {"deploy/capacity/continuous-capacity.sql.candidate", approval.Candidate}} {
		data, e := os.ReadFile(filepath.Join(root(), candidate.path))
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != candidate.hash {
			t.Fatal("unapproved candidate")
		}
		if _, e = db.Exec(ctx, string(data)); e != nil {
			t.Fatal("approved isolated candidate", e)
		}
	}
}

func capacityReporter(t *testing.T, db *pgxpool.Pool, manifest string) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	_, e := db.Exec(ctx, `CREATE ROLE capacity_reporter LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_storage_reporter;
 ALTER ROLE capacity_reporter SET statement_timeout='2s';ALTER ROLE capacity_reporter SET transaction_timeout='2s';ALTER ROLE capacity_reporter SET lock_timeout='250ms'`)
	if e != nil {
		t.Fatal(e)
	}
	cfg := db.Config().Copy()
	cfg.ConnConfig.User = "capacity_reporter"
	cfg.ConnConfig.Password = "disposable_fixture_only"
	cfg.MaxConns = 1
	reporter, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(reporter.Close)
	var inc string
	if db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&inc) != nil {
		t.Fatal("incarnation")
	}
	if _, e = db.Exec(ctx, `INSERT INTO scheduler_private.storage_registration VALUES(true,'capacity_reporter',$1,1,pg_postmaster_start_time(),clock_timestamp()+interval '1 hour',$2::jsonb)`, inc, manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `INSERT INTO scheduler_private.storage_state(singleton) VALUES(true)`); e != nil {
		t.Fatal(e)
	}
	return reporter, inc
}

func TestCapacityAdmissionPG(t *testing.T) {
	db := openDB(t)
	capacitySchema(t, db)
	ctx := context.Background()
	r, inc := capacityReporter(t, db, `[{"device":"1:2","fsid":"fixture"}]`)
	client := storagecontrol.Client{DB: r, Incarnation: inc}
	allowed := func(want bool) {
		t.Helper()
		var a bool
		if e := db.QueryRow(ctx, `SELECT scheduler_private.storage_admission(clock_timestamp())`).Scan(&a); e != nil || a != want {
			t.Fatal("admission", a, want, e)
		}
	}
	allowed(false)
	publish := func(used, avail int64) {
		t.Helper()
		ch, e := client.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		e = client.Publish(ctx, ch, map[string]any{"sequence": ch.Sequence, "filesystems": []any{map[string]any{"device": "1:2", "fsid": "fixture", "total": 1000, "used": used, "available": avail, "inodes": 1000, "free_inodes": 900}}})
		if e != nil {
			t.Fatal(e)
		}
	}
	publish(100, 900)
	allowed(false)
	publish(100, 900)
	allowed(true)
	publish(800, 200)
	allowed(false) // exact80%, no bypass.
	publish(100, 900)
	allowed(false)
	publish(100, 900)
	allowed(true)
	if e := client.Unavailable(ctx); e != nil {
		t.Fatal(e)
	}
	allowed(false)
	publish(100, 900)
	publish(100, 900)
	allowed(true)
	time.Sleep(5100 * time.Millisecond)
	allowed(false) // real TTL, no clock mutation.
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	_, e := w.Exec(ctx, `SELECT scheduler_private.storage_begin($1::uuid)`, wi)
	budgetSQLState(t, e, "42501")
	_, e = r.Exec(ctx, `SELECT * FROM public.monitor_runs`)
	budgetSQLState(t, e, "42501")
	_, e = r.Exec(ctx, `UPDATE public.scheduler_control SET disk_used_bytes=0`)
	budgetSQLState(t, e, "42501")
	_, e = r.Exec(ctx, `SELECT * FROM scheduler_private.storage_state`)
	budgetSQLState(t, e, "42501")
	t.Log("protocol-injected thresholds only; no target execution; real TTL; no runtime DML/row privileges")
}
