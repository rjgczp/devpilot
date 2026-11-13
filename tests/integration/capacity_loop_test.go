//go:build integration && schedulerfixture && privateworker && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture && privateloopfixture && capacityfixture

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/storagecontrol"
	"github.com/rjgczp/devpilot/internal/user"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func init() {
	privateCapacitySetup = setupPrivateCapacity
	privateCapacityHold = holdCapacityUnknown
	privateCapacitySession = capacityOwnerSession
}
func setupPrivateCapacity(t *testing.T, db *pgxpool.Pool, p *periodicPipe, w *pgxpool.Pool, wi string, c *pgxpool.Pool, ci string) {
	ctx := context.Background()
	a, e := os.ReadFile(filepath.Join(root(), "deploy/capacity/isolated-test-approval.json"))
	if e != nil {
		t.Fatal(e)
	}
	var approval struct {
		Hash       string `json:"candidate_sha256"`
		Production bool   `json:"production"`
	}
	if json.Unmarshal(a, &approval) != nil || approval.Production {
		t.Fatal("approval")
	}
	data, e := os.ReadFile(filepath.Join(root(), "deploy/capacity/continuous-capacity.sql.candidate"))
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != approval.Hash {
		t.Fatal("unapproved capacity candidate")
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(data)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='private-isolated',emergency_stop=false WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	var ready bool
	if e = w.QueryRow(ctx, `SELECT scheduler_private.private_worker_ready($1::uuid)`, wi).Scan(&ready); e != nil || ready {
		t.Fatal("missing sample admitted Worker", e)
	}
	var topology bool
	if e = db.QueryRow(ctx, `SELECT current_setting('data_directory')='/var/lib/postgresql/18/docker' AND current_setting('temp_tablespaces')='' AND current_setting('archive_mode')='off' AND current_setting('logging_collector')='off' AND current_setting('log_destination')='stderr' AND NOT EXISTS(SELECT 1 FROM pg_tablespace WHERE pg_tablespace_location(oid)<>'') AND NOT EXISTS(SELECT 1 FROM pg_replication_slots)`).Scan(&topology); e != nil || !topology {
		t.Fatal("unbound PG storage topology", e)
	}
	bindings := p.exchange("capacity-filesystems", nil)
	reporter, inc := capacityReporter(t, db, string(bindings.Frame))
	if _, e = db.Exec(ctx, `UPDATE scheduler_private.storage_registration SET expires_at=clock_timestamp()+interval '4 hours'`); e != nil {
		t.Fatal(e)
	}
	privateCapacityDeny = func(t *testing.T) {
		t.Helper()
		// Keep this unavailable window real: a still-running sampler could
		// restore eligibility before the protected-path assertions finish.
		p.exchange("worker-capacity-stop-sampling", nil)
		if e := (storagecontrol.Client{DB: reporter, Incarnation: inc}).Unavailable(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	dsn := func(pool *pgxpool.Pool) string {
		cc := pool.Config().ConnConfig
		return fmt.Sprintf("postgres://%s:%s@pg:5432/devpilot_registration_test?sslmode=disable", cc.User, cc.Password)
	}
	p.exchange("worker-capacity-start-sources", map[string]string{"reporter_dsn": dsn(reporter), "reporter_incarnation": inc, "controller_dsn": dsn(c), "controller_incarnation": ci})
	ok := false
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(100 * time.Millisecond) {
		if e = db.QueryRow(ctx, `SELECT scheduler_private.storage_admission(clock_timestamp())`).Scan(&ok); e != nil {
			t.Fatal(e)
		}
		if ok {
			break
		}
	}
	if !ok {
		var raw []byte
		_ = db.QueryRow(ctx, `SELECT jsonb_build_object('status',status,'relation_bytes',relation_bytes,'metrics',metrics,'received_at',received_at,'valid_until',valid_until) FROM scheduler_private.storage_state`).Scan(&raw)
		t.Log("actual storage failure", string(raw))
		t.Fatal("real bound filesystem sample never became healthy; do not override")
	}
	t.Log("actual filesystem metadata bound; no capacity/lower-layer reservation claim")
}

func holdCapacityUnknown(t *testing.T, db *pgxpool.Pool, p *periodicPipe, f memberFixture, primary monitor.Public, w *pgxpool.Pool, wi string, s *pgxpool.Pool, si string, c *pgxpool.Pool, ci string) string {
	ctx := context.Background()
	input := monitorInput("capacity-protected-unknown")
	url := primary.URL
	input.URL = &url
	b, e := (monitor.Store{Pool: db}).Create(ctx, f.od, primary.WorkspaceID, primary.ProjectID, input)
	if e != nil {
		t.Fatal(e)
	}
	_ = budgetHint(t, db, f, b)
	worker := scheduler.NewPrivateWorker(scheduler.Store{DB: w, Incarnation: wi})
	hints, e := worker.Candidates(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	var hint scheduler.Hint
	for _, h := range hints {
		if h.MonitorID == b.ID {
			hint = h
		}
	}
	lease, e := worker.Claim(ctx, hint)
	if e != nil {
		t.Fatal(e)
	}
	var ns string
	db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&ns)
	line := handoff.Lineage{WorkspaceID: lease.WorkspaceID, ProjectID: lease.ProjectID, MonitorID: lease.MonitorID, RunID: lease.RunID, ConfigurationVersion: lease.ConfigurationVersion, ActivationGeneration: lease.ActivationGeneration, SchedulerEpoch: lease.SchedulerEpoch, FencingToken: lease.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: si}
	prepared := p.exchange("prepare", line)
	proof, _ := json.Marshal(prepared.Ready)
	digest := sha256.Sum256(proof)
	if _, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, wi, ns, digest[:]); e != nil {
		t.Fatal(e)
	}
	if _, e = worker.Start(ctx, hint, lease); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Exec(ctx, quarantinePhysicalSQL, ci, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.FencingToken); e != nil {
		t.Fatal(e)
	}
	if _, e = (scheduler.Store{DB: c, Incarnation: ci}).RecoverStarted(ctx, line, "worker_lost"); e != nil {
		t.Fatal(e)
	}
	p.exchange("worker-capacity-hold-unknown", line)
	return lease.RunID
}

func capacityOwnerSession(t *testing.T, db *pgxpool.Pool, f *memberFixture) {
	t.Helper()
	secret, digest, e := session.NewTokens().New()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = (session.Store{Pool: db}).Create(context.Background(), user.Credentials{Public: f.owner, PasswordHash: password.DummyPHC}, digest, 4*time.Hour); e != nil {
		t.Fatal(e)
	}
	f.ownerToken = secret
	f.od = digest
}
