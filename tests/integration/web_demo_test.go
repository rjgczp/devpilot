//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

func TestWebDemoPG(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	w1 = periodicPrivateLogin(t, db, w1)
	w2 = periodicPrivateLogin(t, db, w2)
	s = periodicPrivateLogin(t, db, s)
	c = periodicPrivateLogin(t, db, c)
	p := &periodicPipe{t: t, in: periodicScanner(), prefix: "NETWORK64 "}
	catalog := p.exchange("catalog", nil)
	f, m := monitorFixture(t, db)
	url := "https://a.fixture.example.com/fixture/private-path-SENTINEL"
	interval, timeout := int64(60), int64(10)
	var e error
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &url, IntervalSeconds: &interval, TimeoutSeconds: &timeout}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	boot := p.exchange("demo-bootstrap", nil)
	var credentials struct {
		Password string `json:"password"`
	}
	credentials.Password = string(boot.Frame)
	if len(credentials.Password) < 24 {
		t.Fatal("private demo password required")
	}
	hash, err := password.New().Hash(credentials.Password)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, hash, f.owner.ID); err != nil {
		t.Fatal(err)
	}
	name := "本地演示 · 隔离 HTTPS fixture"
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	stopAPI := webDemoServer(t, db, m)
	defer stopAPI()
	p.exchange("demo-ready", map[string]string{"email": f.owner.Email, "workspace_id": m.WorkspaceID, "project_id": m.ProjectID, "monitor_id": m.ID})
	// Only the human's existing Owner HTTP authorization can open this schedule.
	authorized := false
	for until := time.Now().Add(12 * time.Minute); time.Now().Before(until); time.Sleep(500 * time.Millisecond) {
		if err = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM monitor_schedules WHERE monitor_id=$1 AND authorized_configuration_version=$2::bigint AND expires_at>clock_timestamp())`, m.ID, m.Version).Scan(&authorized); err != nil {
			t.Fatal(err)
		}
		if authorized {
			break
		}
	}
	if !authorized {
		t.Fatal("demo Owner authorization deadline reached; no automatic authorization")
	}
	server, client, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	baseline := status75ReadPair(t, client, server.URL, m, f.ownerToken)
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET stop_public_key=$1 WHERE kind='supervisor' AND incarnation=$2`, []byte(catalog.PublicKey), si); e != nil {
		t.Fatal(e)
	}
	ip, e := netip.ParseAddr(catalog.IP)
	if e != nil {
		t.Fatal(e)
	}
	dir, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32))}, []scheduler.FixtureTarget{{URL: m.URL, Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	initial := scheduler.Hint{WorkspaceID: m.WorkspaceID, ProjectID: m.ProjectID, MonitorID: m.ID, ConfigurationVersion: m.Version}
	entry, e := dir.Bind(initial, m.URL, ip)
	if e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	boots := []periodicBoot{{fmt.Sprintf("postgres://scheduler_claim_worker1:%s@pg:5432/devpilot_registration_test?sslmode=disable", w1.Config().ConnConfig.Password), i1, catalog.IP}, {fmt.Sprintf("postgres://scheduler_claim_worker2:%s@pg:5432/devpilot_registration_test?sslmode=disable", w2.Config().ConnConfig.Password), i2, catalog.IP}}
	p.exchange("workers-start", boots)
	var previousDue *time.Time
	var previousRun string
	began := time.Now()
	attempts := [2]int{}
	cyclesDone := 0
demoCycles:
	for cycle := 0; cycle < 3; cycle++ {
		winner := -1
		var tick periodicTick
		for winner < 0 {
			var enabled bool
			if e = db.QueryRow(ctx, `SELECT desired_state='enabled' FROM monitor_activation_states WHERE monitor_id=$1`, m.ID).Scan(&enabled); e != nil {
				t.Fatal(e)
			}
			if !enabled {
				break demoCycles
			}
			if time.Since(began) > 240*time.Second {
				t.Fatal("demo execution deadline reached (possibly paused); no retries or forged release")
			}
			messages := p.exchange("workers-tick", nil).Workers
			if len(messages) != 2 {
				t.Fatal("two independent Workers required")
			}
			for n, msg := range messages {
				if msg.Op != "tick" {
					t.Fatal("Worker did not complete tick")
				}
				var v periodicTick
				periodicDecode(t, msg.Payload, &v)
				if v.Login != []string{"scheduler_claim_worker1", "scheduler_claim_worker2"}[n] {
					t.Fatal("Worker identity mismatch")
				}
				attempts[n]++
				if v.Lease != nil {
					if winner >= 0 {
						t.Fatal("same cycle claimed twice")
					}
					winner = n
					tick = v
				}
			}
			if winner < 0 {
				time.Sleep(500 * time.Millisecond)
			}
		}
		lease := *tick.Lease
		if lease.RunID == previousRun || lease.FencingToken != fmt.Sprint(cycle+1) || lease.ConfigurationVersion != baseline.a.ConfigurationVersion {
			t.Fatal("Run/fence/Owner lineage changed")
		}
		if previousDue != nil && lease.ClaimedAt.Before(*previousDue) {
			t.Fatal("Claim preceded actual next due")
		}
		var ns string
		if db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&ns) != nil {
			t.Fatal("namespace identity")
		}
		line := handoff.Lineage{WorkspaceID: lease.WorkspaceID, ProjectID: lease.ProjectID, MonitorID: lease.MonitorID, RunID: lease.RunID, ConfigurationVersion: lease.ConfigurationVersion, ActivationGeneration: lease.ActivationGeneration, SchedulerEpoch: lease.SchedulerEpoch, FencingToken: lease.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: si}
		ready := p.exchange("prepare", line)
		if len(ready.Ready) != 2 {
			t.Fatal("independent default-deny guards missing")
		}
		proof, _ := json.Marshal(ready.Ready)
		digest := sha256.Sum256(proof)
		_, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, boots[winner].Incarnation, ns, digest[:])
		if e != nil {
			t.Fatal(e)
		}
		msg := p.exchange("worker-call", map[string]any{"index": winner, "command": map[string]any{"op": "execute", "payload": periodicExecution{line}}}).Worker
		for msg.Op != "done" {
			assertNoDBDuringInput(t, db, w1, s)
			assertNoDBDuringInput(t, db, w2, s)
			var answer periodicReply
			switch msg.Op {
			case "consume-input":
				var requested handoff.Lineage
				periodicDecode(t, msg.Payload, &requested)
				if requested != line {
					t.Fatal("unbound Supervisor input request")
				}
				func() {
					ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
					defer cancel()
					tx, err := s.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(context.Background())
					if _, err = tx.Exec(ctx, periodicConsumeSQL, si, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, ns, line.FencingToken); err != nil {
						t.Fatal(err)
					}
					if err = tx.Commit(ctx); err != nil {
						t.Fatal("input acknowledgement unknown", err)
					}
				}()
				answer.Event = "input_committed"
			case "finish":
				_, e = c.Exec(ctx, quarantinePhysicalSQL, ci, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.FencingToken)
				if e != nil {
					t.Fatal(e)
				}
				answer = p.exchange("finish", nil)
				verified, err := supervisorfixture.VerifyCommittedStopBundle(answer.Receipt, []byte(catalog.PublicKey), answer.Bundle, line)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, verified); err != nil {
					t.Fatal(err)
				}
			case "launch", "next", "install-joint", "reply", "replay":
				answer = p.exchange(msg.Op, msg.Payload)
			default:
				t.Fatal("unsupported Worker request", msg.Op)
			}
			msg = p.exchange("worker-send", map[string]any{"index": winner, "response": answer}).Worker
		}
		var evidence []byte
		var due *time.Time
		var finished, started time.Time
		var nextID string
		if e = db.QueryRow(ctx, `SELECT s.next_due_at,r.finished_at,r.started_at,s.next_run_id::text,jsonb_build_object('event','cycle_completed','at',clock_timestamp(),'cycle',$2::int,'worker',r.lease_owner,'run',to_jsonb(r),'schedule',to_jsonb(s),'reservation',to_jsonb(q)) FROM monitor_runs r JOIN monitor_schedules s USING(workspace_id,project_id,monitor_id) JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.run_id=$1 AND r.attempt_state='completed' AND r.http_status=200 AND r.success AND r.failure_code IS NULL AND r.result_commit_id IS NOT NULL AND r.duration_ms BETWEEN 0 AND 10000 AND r.physical_release_confirmed_at=q.released_at AND q.reservation_state='released' AND q.release_basis='supervisor'`, line.RunID, cycle+1).Scan(&due, &finished, &started, &nextID, &evidence); e != nil {
			t.Fatal("minimal persisted Run/Result/due contract", e)
		}
		if (due != nil && (due.Before(finished.Add(60*time.Second)) || due.Before(started.Add(60*time.Second)))) || nextID == line.RunID {
			t.Fatal("next due did not preserve real60s interval")
		}
		p.exchange("cycle-completed", json.RawMessage(evidence))
		previousDue = due
		cyclesDone++
		previousRun = line.RunID
	}
	if attempts[0] < cyclesDone || attempts[1] < cyclesDone {
		t.Fatal("both Workers did not participate in all cycles")
	}
	p.exchange("workers-stop", nil)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != cyclesDone || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 {
		t.Fatal("unexpected extra Run or retained physical slot")
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE kind='deployment'`); e != nil {
		t.Fatal(e)
	}
	p.exchange("demo-completed", map[string]int{"cycles": cyclesDone})
	// Retain the real API/DB for viewing, with execution hard closed.
	p.exchange("demo-view", nil)
}
