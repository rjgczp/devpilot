//go:build integration && schedulerfixture && privateworker && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture && privateloopfixture

package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
	"github.com/rjgczp/devpilot/internal/workerruntime"
)

type privateWorkerMessage struct {
	Index    int                           `json:"index"`
	Kind     string                        `json:"kind"`
	Request  workerruntime.Call            `json:"request"`
	Event    struct{ Event, RunID string } `json:"event"`
	ExitCode int                           `json:"exit_code"`
}

func TestPrivateLoopPG(t *testing.T) {
	ctx := context.Background()
	db := openDB(t)
	var cluster string
	if db.QueryRow(ctx, `SHOW cluster_name`).Scan(&cluster) != nil || cluster != "devpilot-budget-disposable" {
		t.Fatal("disposable cluster required")
	}
	goose(t, filepath.Join(root(), "migrations"), "up", true) // Initial Gate remains closed.
	data, e := os.ReadFile(filepath.Join(root(), "deploy/privateloop/private-mode.sql.candidate"))
	if e != nil {
		t.Fatal(e)
	}
	approvalRaw, e := os.ReadFile(filepath.Join(root(), "deploy/privateloop/isolated-test-approval.json"))
	if e != nil {
		t.Fatal(e)
	}
	var approval struct {
		Candidate  string `json:"candidate_sha256"`
		Allowed    bool   `json:"disposable_pg18_local_and_ci"`
		Production bool   `json:"production"`
	}
	json.Unmarshal(approvalRaw, &approval)
	hash := sha256.Sum256(data)
	if !approval.Allowed || approval.Production || approval.Candidate != hex.EncodeToString(hash[:]) {
		t.Fatal("candidate not independently approved")
	}
	if _, e = db.Exec(ctx, string(data)); e != nil {
		t.Fatal("approved isolated candidate", e)
	}
	if _, e = db.Exec(ctx, `REVOKE TEMP ON DATABASE devpilot_registration_test FROM PUBLIC`); e != nil {
		t.Fatal(e)
	}
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	w1 = periodicPrivateLogin(t, db, w1)
	w2 = periodicPrivateLogin(t, db, w2)
	s = periodicPrivateLogin(t, db, s)
	c = periodicPrivateLogin(t, db, c)
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET execution_mode='private-isolated'; UPDATE scheduler_control SET gate='private-isolated',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	// Prove a registered private identity cannot reuse fixture, even with its grant.
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture' WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	var ready bool
	if e = w1.QueryRow(ctx, `SELECT scheduler_private.private_worker_ready($1::uuid)`, i1).Scan(&ready); e != nil || ready {
		t.Fatal("fixture opened private Worker", e)
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='private-isolated' WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	if e = w1.QueryRow(ctx, `SELECT scheduler_private.private_worker_ready($1::uuid)`, i1).Scan(&ready); e != nil || !ready {
		t.Fatal("private admission unavailable", e)
	}
	p := &periodicPipe{t: t, in: periodicScanner(), prefix: "NETWORK64 "}
	catalog := p.exchange("catalog", nil)
	scenario := catalog.Case
	if scenario == "" {
		scenario = "cycles"
	}
	f, m := monitorFixture(t, db)
	url := "https://a.fixture.example.com/fixture/private-path-SENTINEL"
	interval, timeout := int64(60), int64(10)
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &url, IntervalSeconds: &interval, TimeoutSeconds: &timeout}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	server, client, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	interactive := scenario == "web"
	if interactive {
		boot := p.exchange("demo-bootstrap", nil)
		if len(boot.Frame) < 24 {
			t.Fatal("private demo password required")
		}
		digest, err := password.New().Hash(string(boot.Frame))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(ctx, `UPDATE users SET password_hash=$1 WHERE id=$2`, digest, f.owner.ID); err != nil {
			t.Fatal(err)
		}
		name := "私人隔离闭环 · HTTPS fixture"
		m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m))
		if e != nil {
			t.Fatal(e)
		}
		stop := webDemoServer(t, db, m)
		defer stop()
		p.exchange("demo-ready", map[string]string{"email": f.owner.Email, "workspace_id": m.WorkspaceID, "project_id": m.ProjectID, "monitor_id": m.ID})
		authorized := false
		for until := time.Now().Add(10 * time.Minute); time.Now().Before(until); time.Sleep(500 * time.Millisecond) {
			if e = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM monitor_schedules WHERE monitor_id=$1 AND authorized_configuration_version=$2::bigint AND expires_at>clock_timestamp())`, m.ID, m.Version).Scan(&authorized); e != nil {
				t.Fatal(e)
			}
			if authorized {
				break
			}
		}
		if !authorized {
			t.Fatal("no Owner consent; execution refused")
		}
	} else {
		a, h, _ := checkpointProjectionGet(t, client, server.URL, status75Base(m), f.ownerToken)
		seconds := 900
		if scenario == "expiry" {
			seconds = 60
		}
		code, _, raw := checkpointHTTP(t, client, server.URL, "PUT", status75Base(m)+"/execution-authorization", fmt.Sprintf(`{"configuration_version":%q,"consent_profile":"periodic-http-get-v1","authorization_seconds":%d}`, a.ConfigurationVersion, seconds), f.ownerToken, h.Get("ETag"), nil)
		if code != 200 {
			t.Fatal("real Owner consent", code, string(raw))
		}
	}
	baseline := status75ReadPair(t, client, server.URL, m, f.ownerToken)
	if scenario == "pause" {
		code, _, raw := checkpointHTTP(t, client, server.URL, "PUT", status75Base(m)+"/activation", `{"desired_state":"disabled","configuration_version":"`+m.Version+`"}`, f.ownerToken, baseline.tag, nil)
		if code != 200 {
			t.Fatal("Owner pause", code, string(raw))
		}
	}
	if scenario == "expiry" {
		var expiry time.Time
		if db.QueryRow(ctx, `SELECT expires_at FROM monitor_schedules WHERE monitor_id=$1`, m.ID).Scan(&expiry) != nil {
			t.Fatal("expiry")
		}
		// Wait outside any SQL transaction; each clock query retains its2s cap.
		if delay := time.Until(expiry); delay > 0 {
			time.Sleep(delay)
		}
		waitWorkspaceTime(t, db, expiry.Add(100*time.Millisecond)) // Real60s, no clock rewrite.
		t.Log("real Owner authorization expiry reached without clock mutation")
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET stop_public_key=$1 WHERE kind='supervisor' AND incarnation=$2`, []byte(catalog.PublicKey), si); e != nil {
		t.Fatal(e)
	}
	ip, e := netip.ParseAddr(catalog.IP)
	if e != nil {
		t.Fatal(e)
	}
	directory, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32))}, []scheduler.FixtureTarget{{URL: m.URL, Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := directory.Bind(scheduler.Hint{WorkspaceID: m.WorkspaceID, ProjectID: m.ProjectID, MonitorID: m.ID, ConfigurationVersion: m.Version}, m.URL, ip)
	if e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	boots := []periodicBoot{{fmt.Sprintf("postgres://scheduler_claim_worker1:%s@pg:5432/devpilot_registration_test?sslmode=disable", w1.Config().ConnConfig.Password), i1, catalog.IP}, {fmt.Sprintf("postgres://scheduler_claim_worker2:%s@pg:5432/devpilot_registration_test?sslmode=disable", w2.Config().ConnConfig.Password), i2, catalog.IP}}
	p.exchange("worker-private-start", boots)
	defer p.exchange("worker-private-stop", nil)
	lines := map[int]handoff.Lineage{}
	committed := 0
	finishWeb := func() {
		p.exchange("worker-private-stop", nil)
		if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
			t.Fatal(e)
		}
		p.exchange("demo-completed", map[string]int{"cycles": committed})
		p.exchange("demo-view", nil)
	}
	want := 3
	if scenario == "lost" {
		want = 1
	}
	began := time.Now()
	var previousDue *time.Time
	unknown := false
	var restartedAt time.Time
	seenReady := map[int]bool{}
	for time.Since(began) < 320*time.Second {
		msg := p.exchange("worker-private-poll", nil).Worker
		if msg.Op == "" {
			if !restartedAt.IsZero() && time.Since(restartedAt) > 3*time.Second {
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 2 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
					t.Fatal("restart changed unknown Run or returned slot")
				}
				p.exchange("worker-negative-complete", map[string]any{"case": "lost", "unknown": true, "capacity_released": false, "restart_no_resend": true})
				return
			}
			if interactive && len(seenReady) == 2 {
				var enabled bool
				if e = db.QueryRow(ctx, `SELECT desired_state='enabled' FROM monitor_activation_states WHERE monitor_id=$1`, m.ID).Scan(&enabled); e != nil {
					t.Fatal(e)
				}
				if !enabled {
					finishWeb()
					return
				}
			}
			if (scenario == "pause" || scenario == "expiry") && time.Since(began) > 3*time.Second && len(seenReady) == 2 {
				if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
					t.Fatal("paused/expired schedule executed")
				}
				p.exchange("worker-negative-complete", map[string]any{"case": scenario})
				return
			}
			continue
		}
		var message privateWorkerMessage
		periodicDecode(t, msg.Payload, &message)
		index := message.Index
		if index < 0 || index > 1 {
			t.Fatal("worker index")
		}
		if msg.Op == "private-event" {
			if message.Event.Event == "worker_ready" {
				seenReady[index] = true
			}
			if message.Event.Event == "result_committed" {
				line := lines[index]
				committed++
				pair := status75ReadPair(t, client, server.URL, m, f.ownerToken)
				if pair.s.InFlight || pair.s.CurrentObservation == nil || pair.s.NextDue == nil {
					t.Fatal("private Result projection")
				}
				var evidence []byte
				var due, finished, started time.Time
				var next string
				e = db.QueryRow(ctx, `SELECT s.next_due_at,r.finished_at,r.started_at,s.next_run_id::text,jsonb_build_object('cycle',$2::int,'run',to_jsonb(r),'schedule',to_jsonb(s),'reservation',to_jsonb(q)) FROM monitor_runs r JOIN monitor_schedules s USING(workspace_id,project_id,monitor_id) JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.run_id=$1 AND r.attempt_state='completed' AND r.success AND r.http_status=200 AND r.physical_release_confirmed_at=q.released_at`, line.RunID, committed).Scan(&due, &finished, &started, &next, &evidence)
				if e != nil || due.Before(finished.Add(60*time.Second)) || next == line.RunID {
					t.Fatal("real private Run/Result/due", e)
				}
				previousDue = &due
				p.exchange("cycle-completed", json.RawMessage(evidence))
				code, _, body := checkpointHTTP(t, client, server.URL, "GET", status75Base(m)+"/history", "", f.ownerToken, "", nil)
				var page monitor.HistoryPage
				if code != 200 || json.Unmarshal(body, &page) != nil || len(page.Data) != committed || page.Data[0].Status != "success" || page.Data[0].DurationMS == nil {
					t.Fatal("Dashboard real history", code, string(body))
				}
				if (scenario == "cycles" || interactive) && committed == want && len(seenReady) == 2 {
					if interactive {
						finishWeb()
					}
					return
				}
			}
			continue
		}
		if msg.Op == "private-exit" {
			if scenario == "lost" && unknown {
				pair := status75ReadPair(t, client, server.URL, m, f.ownerToken)
				if pair.s.CurrentObservation != nil {
					t.Fatal("old success overwrote unknown")
				}
				code, _, body := checkpointHTTP(t, client, server.URL, "GET", status75Base(m)+"/history", "", f.ownerToken, "", nil)
				var page monitor.HistoryPage
				if code != 200 || json.Unmarshal(body, &page) != nil || len(page.Data) != 2 || page.Data[0].Status != "unknown" || page.Data[0].HTTPStatus != nil || page.Data[1].Status != "success" {
					t.Fatal("unknown history isolation", string(body))
				}
				if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
					t.Fatal("untrusted termination returned slot")
				}
				p.exchange("worker-private-restart", map[string]int{"index": index})
				restartedAt = time.Now()
				continue
			}
			t.Fatal("unexpected native Worker exit", message.ExitCode)
		}
		if msg.Op != "private-request" {
			t.Fatal("unsupported native event", msg.Op)
		}
		call := message.Request
		reply := workerruntime.Reply{Event: "error", Line: lines[index]}
		switch call.Op {
		case "hello":
			reply.Event = "supervisor_ready"
		case "prepare":
			if !restartedAt.IsZero() {
				t.Fatal("unknown Run replayed after restart")
			}
			if call.Lease == nil || call.Hint == nil {
				t.Fatal("missing Claim")
			}
			lease := *call.Lease
			if previousDue != nil && lease.ClaimedAt.Before(*previousDue) {
				t.Fatal("private Claim before due")
			}
			var namespace string
			db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&namespace)
			line := handoff.Lineage{WorkspaceID: lease.WorkspaceID, ProjectID: lease.ProjectID, MonitorID: lease.MonitorID, RunID: lease.RunID, ConfigurationVersion: lease.ConfigurationVersion, ActivationGeneration: lease.ActivationGeneration, SchedulerEpoch: lease.SchedulerEpoch, FencingToken: lease.FencingToken, NamespaceIncarnation: namespace, SupervisorIncarnation: si}
			if lease.MonitorID != m.ID || lease.ConfigurationVersion != m.Version || lease.FencingToken != fmt.Sprint(committed+1) {
				t.Fatal("private Fence/Owner lineage")
			}
			prepared := p.exchange("prepare", line)
			if len(prepared.Ready) != 2 {
				t.Fatal("independent guards")
			}
			proof, _ := json.Marshal(prepared.Ready)
			digest := sha256.Sum256(proof)
			if _, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, boots[index].Incarnation, namespace, digest[:]); e != nil {
				t.Fatal(e)
			}
			lines[index] = line
			reply.Line = line
			reply.Event = "prepared"
		case "consume":
			line := lines[index]
			if call.Line == nil || *call.Line != line {
				t.Fatal("input binding")
			}
			if _, e = s.Exec(ctx, periodicConsumeSQL, si, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.NamespaceIncarnation, line.FencingToken); e != nil {
				t.Fatal(e)
			}
			reply.Event = "input_committed"
		case "launch", "next", "reply":
			line := lines[index]
			if call.Line == nil || *call.Line != line {
				t.Fatal("lifecycle binding")
			}
			var payload any
			if call.Op == "launch" {
				payload = map[string][]byte{"envelope": call.Envelope, "receipt": call.Receipt}
			}
			if call.Op == "reply" {
				payload = map[string][]byte{"frame": call.Frame}
			}
			answer := p.exchange(call.Op, payload)
			reply.Event = answer.Event
			reply.Frame = answer.Frame
			reply.Observation = answer.Result
		case "install":
			if call.Permit == nil || call.Request == nil || call.Request.Lineage != lines[index] {
				t.Fatal("target binding")
			}
			answer := p.exchange("install-joint", supervisorfixture.Install{Request: *call.Request, PermitID: call.Permit.PermitID, CommittedAt: call.Permit.CommittedAt, PermitDeadline: call.Permit.PermitDeadline})
			reply.Event = "installed"
			reply.Confirmation = &answer.Confirmation
		case "settle":
			line := lines[index]
			if call.Line == nil || *call.Line != line {
				t.Fatal("settlement binding")
			}
			if _, e = c.Exec(ctx, quarantinePhysicalSQL, ci, line.WorkspaceID, line.ProjectID, line.MonitorID, line.RunID, line.FencingToken); e != nil {
				t.Fatal(e)
			}
			if scenario == "lost" && committed == want {
				p.exchange("worker-kill-supervisor", nil)
				if _, e = (scheduler.Store{DB: c, Incarnation: ci}).RecoverStarted(ctx, line, "worker_lost"); e != nil {
					t.Fatal("unknown recovery", e)
				}
				unknown = true
				reply.Event = "supervisor_lost"
			} else {
				stopped := p.exchange("finish", nil)
				verified, e := supervisorfixture.VerifyCommittedStopBundle(stopped.Receipt, []byte(catalog.PublicKey), stopped.Bundle, line)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, verified); e != nil {
					t.Fatal(e)
				}
				reply.Event = "settled"
			}
		default:
			t.Fatal("unsupported native operation", call.Op)
		}
		p.exchange("worker-private-reply", map[string]any{"index": index, "reply": reply})
	}
	t.Fatal("bounded private loop did not complete")
}
