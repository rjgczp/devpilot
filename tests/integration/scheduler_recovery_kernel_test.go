//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && resultfixture && recoveryfixture

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

// Optional observation barrier after independent verification, before the
// existing settlement transaction. Nil preserves every historical test path.
var recoveryBeforeSettlement func(*testing.T, *pgxpool.Pool, scheduler.Store, handoff.Lineage)

// Separate recovery orchestration; the original 6.3 driver is unchanged.
func runRecoveryKernel(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	recoveryController = scheduler.Store{DB: c, Incarnation: ci}
	broker := &jointBroker{t: t, db: db, w: w, s: s, in: bufio.NewScanner(os.Stdin)}
	broker.in.Buffer(make([]byte, 4096), 32769)
	recoveryExchange = broker.exchange
	catalog := broker.exchange("catalog", nil)
	scenario := catalog.Case
	inflight := scenario == "tls-stop" || scenario == "tracer-stopped" || scenario == "tracer-crashed" || scenario == "supervisor-killed" || scenario == "stop-ack-lost" || scenario == "journal-collision"
	unconfirmed := recoveryMode == "zombie" || scenario == "tracer-crashed" || scenario == "supervisor-killed" || scenario == "stop-ack-lost" || scenario == "journal-collision"
	allowed := map[string]bool{}
	for _, name := range []string{"allow-http", "allow-https", "tls-stop", "tracer-stopped", "tracer-crashed", "supervisor-killed", "stop-ack-lost", "journal-collision", "permit-missing", "permit-timeout", "target-commit-unknown", "supervisor-lost", "rules-missing", "old-fence", "old-incarnation", "cross-run", "duplicate-envelope", "cross-run-envelope", "rebinding", "ipv6-poison", "tls-invalid", "start-commit-unknown", "input-commit-unknown"} {
		allowed[name] = true
	}
	if !allowed[scenario] {
		t.Fatal("unlisted joint scenario")
	}
	f, m := monitorFixture(t, db)
	scheme := "https"
	if scenario == "allow-http" {
		scheme = "http"
	}
	url := scheme + "://a.fixture.example.com/fixture/private-path-SENTINEL"
	seconds := int64(10)
	var e error
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &url, TimeoutSeconds: &seconds}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	h := recoveryHint(t, db, f, m)
	if recoverySetup != nil {
		recoverySetup(t, db, m)
	}
	_, e = db.Exec(ctx, `UPDATE scheduler_control SET stop_public_key=$1 WHERE kind='supervisor' AND incarnation=$2`, []byte(catalog.PublicKey), si)
	if e != nil {
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
	entry, e := directory.Bind(checkpointTwoHint(h), m.URL, ip)
	if e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	store := scheduler.Store{DB: w, Incarnation: wi}
	for recoveryCycle = 0; recoveryCycle < recoveryCycles; recoveryCycle++ {
		recoveryExpected = "started"
		lease, e := store.Claim(ctx, checkpointTwoHint(h))
		if e != nil {
			t.Fatal(e)
		}
		quarantine := func() {
			t.Helper()
			_, err := c.Exec(ctx, quarantinePhysicalSQL, ci, h.w, h.p, h.m, h.r, lease.FencingToken)
			budgetSQLState(t, err, "00000")
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND reservation_state='quarantined' AND termination_receipt IS NULL AND released_at IS NULL`, h.r) != 1 {
				t.Fatal("quarantine did not preserve physical capacity")
			}
		}
		var ns string
		if db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&ns) != nil {
			t.Fatal("namespace identity")
		}
		line := handoff.Lineage{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m, RunID: h.r, ConfigurationVersion: h.v, ActivationGeneration: h.g, SchedulerEpoch: h.e, FencingToken: lease.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: si}
		if recoveryCheck != nil {
			unprepared := line
			unprepared.NamespaceIncarnation, unprepared.SupervisorIncarnation = "", ""
			recoveryCheck(t, "claimed", db, store, unprepared, nil)
		}
		ready := broker.exchange("prepare", line)
		if len(ready.Ready) != 2 {
			t.Fatal("independent rules not prepared")
		}
		proof, _ := json.Marshal(ready.Ready)
		digest := sha256.Sum256(proof)
		_, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, h.w, h.p, h.m, h.r, wi, ns, digest[:])
		budgetSQLState(t, e, "00000")
		startStore := store
		if scenario == "start-commit-unknown" {
			startStore.DB = workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
		}
		snapshot, e := startStore.Start(ctx, checkpointTwoHint(h), lease)
		if scenario == "start-commit-unknown" {
			if !errors.Is(e, scheduler.ErrCommitUnknown) {
				t.Fatal("Start unknown not conservative", e)
			}
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='started'`, h.r) != 1 {
				t.Fatal("lost Start must actually commit")
			}
			pipes, err := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
			pipes.Close()
			if !errors.Is(err, scheduler.ErrInputConsumed) {
				t.Fatal("unknown Start exposed input")
			}
			quarantine()
			broker.exchange("finish-no-input", nil)
			if recoveryCheck != nil {
				recoveryCheck(t, "no-input", db, store, line, nil)
			}
			return
		}
		if e != nil {
			t.Fatal(e)
		}
		inputController := scheduler.InputController{DB: s, Incarnation: si}
		if scenario == "input-commit-unknown" {
			inputController.DB = workspaceObservedDB{Pool: s, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
		}
		pipes, e := snapshot.IssueInput(ctx, inputController)
		if scenario == "input-commit-unknown" {
			pipes.Close()
			if !errors.Is(e, scheduler.ErrCommitUnknown) {
				t.Fatal("input unknown")
			}
			pipes, e = snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
			pipes.Close()
			if !errors.Is(e, scheduler.ErrInputConsumed) {
				t.Fatal("input reconstructed after unknown")
			}
			quarantine()
			broker.exchange("finish-no-input", nil)
			if recoveryCheck != nil {
				recoveryCheck(t, "no-input", db, store, line, nil)
			}
			return
		}
		if e != nil {
			t.Fatal(e)
		}
		frame, e := io.ReadAll(pipes.Envelope)
		if e != nil || len(frame) != handoff.FrameSize {
			t.Fatal("input frame")
		}
		ack, e := io.ReadAll(pipes.Receipt)
		pipes.Close()
		if e != nil || len(ack) != handoff.ReceiptSize {
			t.Fatal("input receipt")
		}
		if scenario == "cross-run-envelope" {
			var env handoff.Envelope
			n := int(binary.BigEndian.Uint32(frame[8:12]))
			if json.Unmarshal(frame[12:12+n], &env) != nil {
				t.Fatal("envelope decode")
			}
			env.RunID = ns
			frame, _, e = handoff.Encode(env)
			if e != nil {
				t.Fatal(e)
			}
		}
		launch := map[string][]byte{"envelope": frame, "receipt": ack}
		sealed := broker.exchange("launch", launch)
		if scenario == "cross-run-envelope" {
			if sealed.Event != "input_rejected" {
				t.Fatal("cross Run frame accepted")
			}
			quarantine()
			broker.exchange("finish", nil)
			return
		}
		if sealed.Event != "sealed" {
			t.Fatal("Linux Probe boundary not sealed")
		}
		next := broker.exchange("next", nil)
		if scenario != "rebinding" && scenario != "ipv6-poison" {
			if next.Event != "target_request" {
				t.Fatal("kernel did not validate DNS before requesting permit", next.Event)
			}
			// A stale Fence is rejected both by this original bound request and by the
			// real PG target reservation function; no alternate caller/lookup is used.
			targetStore := store
			if scenario == "target-commit-unknown" {
				targetStore.DB = workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}
			}
			if scenario == "old-incarnation" {
				targetStore.Incarnation = ns
			}
			controller := snapshot.TargetController(targetStore, checkpointTwoHint(h), directory, broker)
			var reply []byte
			if scenario == "permit-timeout" {
				time.Sleep(2100 * time.Millisecond)
			} else {
				reply = jointHandle(t, next.Frame, func(ctx context.Context, r targetpermit.Request) (targetpermit.Confirmation, error) {
					if r.Lineage != line || r.IP != ip || r.Host != "a.fixture.example.com" || r.Port != map[bool]uint16{true: 80, false: 443}[scenario == "allow-http"] {
						t.Fatal("kernel changed DNS-selected literal or original origin")
					}
					if scenario == "permit-missing" {
						return targetpermit.Confirmation{}, targetpermit.ErrDenied
					}
					if scenario == "old-fence" {
						bad := lease
						bad.FencingToken = "999"
						if _, err := store.ReserveTarget(ctx, checkpointTwoHint(h), bad, entry.Keys[0]); err == nil {
							t.Fatal("PG accepted stale Fence")
						}
						r.FencingToken = "999"
					}
					if scenario == "cross-run" {
						r.RunID = ns
					}
					return controller.Authorize(ctx, r)
				})
			}
			replyAck := broker.exchange("reply", map[string][]byte{"frame": reply})
			wantAck := "reply_delivered"
			if reply == nil {
				wantAck = "reply_denied"
			}
			if replyAck.Event != wantAck {
				t.Fatal("fixture confused EOF denial with a delivered permission frame")
			}
			if scenario == "permit-missing" && (broker.installs != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND target_permit_id IS NOT NULL`, h.r) != 0) {
				t.Fatal("missing permit installed or reserved target authority")
			}
			if inflight {
				broker.exchange("held-tls", nil)
			} else {
				next = broker.exchange("next", nil)
			}
		}
		if scenario == "allow-http" || scenario == "allow-https" || scenario == "duplicate-envelope" {
			if next.Result == nil || !next.Result.Success || next.Result.HTTPStatus == nil || *next.Result.HTTPStatus != 200 {
				t.Fatal("original Header-only GET did not complete")
			}
		} else if scenario == "rebinding" || scenario == "ipv6-poison" || scenario == "tls-invalid" {
			want := monitoring.BlockedTarget
			if scenario == "tls-invalid" {
				want = monitoring.TLSFailed
			}
			if next.Result == nil || next.Result.Success || next.Result.HTTPStatus != nil || next.Result.FailureCode == nil || *next.Result.FailureCode != want {
				t.Fatal("original DNS/TLS policy changed")
			}
		} else if !inflight && next.Result != nil {
			t.Fatal("control refusal fabricated HTTP result")
		}
		if scenario == "duplicate-envelope" {
			if broker.exchange("replay", launch).Event != "replay_rejected" {
				t.Fatal("second launch")
			}
		}
		if scenario == "target-commit-unknown" && (broker.installs != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND target_permit_id IS NOT NULL`, h.r) != 1) {
			t.Fatal("lost target ACK must really commit but never install")
		}
		if recoveryMode == "zombie" {
			broker.exchange("hold-zombie", nil)
			for {
				var remaining float64
				if err := db.QueryRow(ctx, `SELECT GREATEST(0,extract(epoch FROM lease_until-clock_timestamp()))::float8 FROM monitor_runs WHERE run_id=$1`, line.RunID).Scan(&remaining); err != nil {
					t.Fatal(err)
				}
				if remaining == 0 {
					break
				}
				time.Sleep(time.Duration(remaining*float64(time.Second)) + 10*time.Millisecond)
			}
			broker.exchange("zombie-witness", nil)
		}
		// Quarantine commits while the original Controller identity is live and
		// before every independent stop/ACK/journal fault. It never returns capacity.
		if recoveryCheck != nil {
			recoveryCheck(t, "before-stop", db, store, line, next.Result)
		}
		quarantine()
		stopped := broker.exchange("finish", nil)
		if unconfirmed {
			if stopped.Event != "unconfirmed_stop_quarantine" || len(stopped.Receipt) != 0 || len(stopped.Bundle) != 0 {
				t.Fatal("uncertain termination produced an attestation")
			}
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND reservation_state='quarantined' AND released_at IS NULL AND termination_receipt IS NULL`, h.r) != 1 || queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state=$2 AND physical_release_confirmed_at IS NULL AND http_status IS NULL AND success IS NULL AND failure_code IS NULL`, h.r, recoveryExpected) != 1 {
				t.Fatal("unconfirmed stop lost quarantine or fabricated HTTP outcome")
			}
			if recoveryCheck != nil {
				recoveryCheck(t, "unconfirmed", db, store, line, nil)
			}
			t.Log("actual injected physical/ACK/journal fault: no receipt, no physical capacity return, no network retry", scenario)
			return
		}
		verified, e := supervisorfixture.VerifyCommittedStopBundle(stopped.Receipt, []byte(catalog.PublicKey), stopped.Bundle, line)
		if e != nil {
			t.Fatal("independent stop proof", e)
		}
		if recoveryBeforeSettlement != nil {
			recoveryBeforeSettlement(t, db, store, line)
		}
		settlement := supervisorfixture.Settlement{DB: s, Incarnation: si}
		if _, e = settlement.Accept(ctx, verified); e != nil {
			t.Fatal(e)
		}
		if _, e = settlement.Accept(ctx, verified); e == nil {
			t.Fatal("physical receipt consumed twice")
		}
		if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 || queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state=$2 AND physical_release_confirmed_at IS NOT NULL AND http_status IS NULL AND success IS NULL AND failure_code IS NULL`, h.r, recoveryExpected) != 1 {
			t.Fatal("release did not preserve unknown HTTP conclusion")
		}
		assertNoDBDuringInput(t, db, w, s)
		t.Log("real original kernel / DNS / permit / Linux isolation / independent stop / PG single settlement; no persistent HTTP result", scenario)
		if recoveryPatch != nil {
			recoveryPatch(t, f, m)
		}
		if recoveryCheck != nil {
			recoveryCheck(t, "released", db, store, line, next.Result)
		}
		if recoveryCycle+1 < recoveryCycles {
			var due time.Time
			if err := db.QueryRow(ctx, `SELECT next_run_id::text,next_due_at FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3`, h.w, h.p, h.m).Scan(&h.r, &due); err != nil {
				t.Fatal(err)
			}
			t.Log("next independent recovery cycle waits for real due", h.r, due)
			for {
				var remaining float64
				if err := db.QueryRow(ctx, `SELECT GREATEST(0,extract(epoch FROM $1::timestamptz-clock_timestamp()))::float8`, due).Scan(&remaining); err != nil {
					t.Fatal(err)
				}
				if remaining == 0 {
					break
				}
				time.Sleep(time.Duration(remaining * float64(time.Second)))
			}
			broker.exchange("next-cycle", nil)
			w2, wi2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
			store = scheduler.Store{DB: w2, Incarnation: wi2}
			w = w2
			wi = wi2
			broker.w = w2
		}
	}

}
