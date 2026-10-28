//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

const acceptPhysicalSQL = `SELECT scheduler_private.accept_termination($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bytea)`
const quarantinePhysicalSQL = `SELECT scheduler_private.quarantine_physical($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint)`

type releaseReply struct {
	Ready        map[string]supervisorfixture.Ready `json:"ready"`
	Confirmation targetpermit.Confirmation          `json:"confirmation"`
	IP           string                             `json:"ip"`
	PublicKey    string                             `json:"stop_public_key"`
	Receipt      json.RawMessage                    `json:"stop_receipt"`
	Bundle       json.RawMessage                    `json:"stop_bundle"`
	LivePIDs     []int                              `json:"live_pids"`
}
type releaseBroker struct {
	t        *testing.T
	db, w, s *pgxpool.Pool
	in       *bufio.Scanner
}

func (b *releaseBroker) exchange(op string, payload any) releaseReply {
	b.t.Helper()
	assertNoDBDuringInput(b.t, b.db, b.w, b.s)
	raw, e := json.Marshal(map[string]any{"op": op, "payload": payload})
	if e != nil {
		b.t.Fatal(e)
	}
	fmt.Println("NETWORK64 " + string(raw))
	if !b.in.Scan() {
		b.t.Fatal("independent release Supervisor unavailable")
	}
	var reply releaseReply
	if json.Unmarshal(b.in.Bytes(), &reply) != nil {
		b.t.Fatal("invalid bounded release confirmation")
	}
	return reply
}
func (b *releaseBroker) InstallTarget(ctx context.Context, r targetpermit.Request, p scheduler.TargetPermit) (targetpermit.Confirmation, error) {
	out := b.exchange("install", supervisorfixture.Install{Request: r, PermitID: p.PermitID, CommittedAt: p.CommittedAt, PermitDeadline: p.PermitDeadline})
	if !out.Confirmation.Matches(r) || ctx.Err() != nil {
		return targetpermit.Confirmation{}, targetpermit.ErrDenied
	}
	return out.Confirmation, nil
}
func releaseState(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint) string {
	t.Helper()
	var raw []byte
	if e := db.QueryRow(context.Background(), `SELECT jsonb_build_object('budgets',(SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b),'run',(SELECT to_jsonb(r) FROM monitor_runs r WHERE run_id=$1),'reservation',(SELECT to_jsonb(q) FROM scheduler_reservations q WHERE run_id=$1),'schedule',(SELECT to_jsonb(s) FROM monitor_schedules s WHERE monitor_id=$2),'activation',(SELECT to_jsonb(a) FROM monitor_activation_states a WHERE monitor_id=$2))`, h.r, h.m).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func TestBudgetPhysicalReleasePG(t *testing.T) {
	db := budgetDB(t)
	ctx := context.Background()
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	f, m := monitorFixture(t, db)
	fixtureURL := "https://a.fixture.example.com/private-path-SENTINEL"
	m, e := (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &fixtureURL}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	h := budgetHint(t, db, f, m)
	broker := &releaseBroker{t: t, db: db, w: w, s: s, in: bufio.NewScanner(os.Stdin)}
	broker.in.Buffer(make([]byte, 4096), 32769)
	catalog := broker.exchange("catalog", nil)
	// Trusted offline fixture registration. No runtime role has this table DML.
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET stop_public_key=$1 WHERE kind='supervisor' AND incarnation=$2`, []byte(catalog.PublicKey), si); e != nil {
		t.Fatal(e)
	}
	releaseACL(t, db, w, c, s, si, ci, h)
	ip, e := netip.ParseAddr(catalog.IP)
	if e != nil {
		t.Fatal(e)
	}
	directory, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32)), 2: []byte(strings.Repeat("n", 32))}, []scheduler.FixtureTarget{{URL: m.URL, Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := directory.Bind(checkpointTwoHint(h), m.URL, ip)
	if e != nil {
		t.Fatal(e)
	}
	// Populate both retained generations using the approved rotation path before
	// any physical slot exists. Both digest generations must later be released.
	old := entry
	old.Keys = append([]scheduler.TargetDigest(nil), entry.Keys[:1]...)

	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{old}), "00000")
	budgetSQLState(t, budgetRotate(t, c, ci, 2, []scheduler.CatalogEntry{entry}), "00000")
	store := scheduler.Store{DB: w, Incarnation: wi}
	lease, e := store.Claim(ctx, checkpointTwoHint(h))
	if e != nil {
		t.Fatal(e)
	}
	var ns string
	if e = db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&ns); e != nil {
		t.Fatal(e)
	}
	l := handoff.Lineage{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m, RunID: h.r, ConfigurationVersion: h.v, ActivationGeneration: h.g, SchedulerEpoch: h.e, FencingToken: lease.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: si}
	ready := broker.exchange("prepare", l)
	if len(ready.Ready) != 2 {
		t.Fatal("missing independent network proofs")
	}
	proof, _ := json.Marshal(ready.Ready)
	digest := sha256.Sum256(proof)
	_, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, h.w, h.p, h.m, h.r, wi, ns, digest[:])
	budgetSQLState(t, e, "00000")
	snapshot, e := store.Start(ctx, checkpointTwoHint(h), lease)
	if e != nil {
		t.Fatal(e)
	}
	pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = handoff.ReadReceipt(pipes.Receipt); e != nil {
		t.Fatal(e)
	}
	pipes.Close()
	request := targetpermit.Request{Lineage: l, Host: "a.fixture.example.com", Scheme: "https", IP: ip, Port: 443, Deadline: time.Now().UTC().Add(20 * time.Second)}
	permit, e := snapshot.TargetController(store, checkpointTwoHint(h), directory, broker).Authorize(ctx, request)
	if e != nil || !permit.Matches(request) {
		t.Fatal("real target permission failed", e)
	}
	zombie := broker.exchange("zombie", nil)
	if len(zombie.LivePIDs) < 3 {
		t.Fatal("no actual live zombie tree")
	}
	// Fill physical global capacity through real Claim, never admin counter DML.
	fill := budgetFixtures(t, db, 33)
	for _, x := range fill[:31] {
		_, e = schedulerClaim(t, w, wi, x)
		budgetSQLState(t, e, "00000")
	}
	// Same monitor cannot acquire a second namespace/Run/input even if disconnected.
	_, e = schedulerClaim(t, w2, i2, h)
	budgetSQLState(t, e, "P0001")
	var now time.Time
	if e = db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if until := lease.LeaseUntil.Sub(now); until > 0 {
		time.Sleep(until + time.Millisecond)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND lease_until<clock_timestamp() AND physical_release_confirmed_at IS NULL`, h.r) != 1 {
		t.Fatal("original45s Lease did not expire")
	}
	before := releaseState(t, db, h)
	_, e = c.Exec(ctx, quarantinePhysicalSQL, ci, h.w, h.p, h.m, h.r, lease.FencingToken)
	budgetSQLState(t, e, "00000")
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 32 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND reservation_state='quarantined' AND termination_receipt IS NULL`, h.r) != 1 {
		t.Fatal("Lease/quarantine prematurely released physical slots")
	}
	t.Log("REAL original45s Lease expired with still-live unprivileged Probe descendants; global32 remains held; no receipt")
	_ = before
	// Pause/PATCH invalidates current v/g/epoch but must not prevent recovery of
	// the old immutable lineage. All user mutation uses the actual Store.
	name := "release-old-configuration"
	_, e = (monitor.Store{Pool: db}).Update(ctx, f.od, h.w, h.p, h.m, monitor.Input{Name: &name}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, h.u); e != nil {
		t.Fatal(e)
	}
	stopped := broker.exchange("stop-zombie", nil)
	verified, e := supervisorfixture.VerifyCommittedStopBundle(stopped.Receipt, []byte(catalog.PublicKey), stopped.Bundle, l)
	if e != nil {
		t.Fatal("actual committed signed proof rejected", e)
	}
	t.Log("independent actual network revoke, pidfd exits, orphan reap, PID1 retirement and committed private proof journal verified")
	// Every corrupted/foreign lineage, unknown key and proof mismatch must fail
	// before a DB call; this is the explicitly approved Supervisor trust boundary.
	for _, field := range []string{"workspace_id", "project_id", "monitor_id", "run_id", "configuration_version", "activation_generation", "scheduler_epoch", "fencing_token", "supervisor_incarnation", "namespace_incarnation"} {
		var frame map[string]any
		_ = json.Unmarshal(stopped.Receipt, &frame)
		line := frame["payload"].(map[string]any)["lineage"].(map[string]any)
		if strings.Contains(line[field].(string), "-") {
			line[field] = fill[32].r
		} else {
			line[field] = "99"
		}
		bad, _ := json.Marshal(frame)
		if _, e = supervisorfixture.VerifyCommittedStopBundle(bad, []byte(catalog.PublicKey), stopped.Bundle, l); e == nil {
			t.Fatal("forged lineage accepted", field)
		}
		_, e = s.Exec(ctx, acceptPhysicalSQL, si, h.w, h.p, h.m, h.r, bad)
		budgetSQLState(t, e, "P0001")
	}
	if _, e = supervisorfixture.VerifyCommittedStopBundle(stopped.Receipt, []byte("unknown key"), stopped.Bundle, l); e == nil {
		t.Fatal("unknown signature key")
	}
	if _, e = supervisorfixture.VerifyCommittedStopBundle(stopped.Receipt, []byte(catalog.PublicKey), []byte(`{"network":[],"process":{},"retired":{}}`), l); e == nil {
		t.Fatal("signature-only proof accepted")
	}
	rates := releaseRates(t, db, h)
	releaseLockStages(t, db, s, si, h, verified)
	releaseFaults(t, db, s, si, h, verified)
	// A current Supervisor stop or replacement incarnation is not a release
	// credential. Restore only this fixture's original trusted registration.
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET emergency_stop=true WHERE kind='supervisor'`); e != nil {
		t.Fatal(e)
	}
	_, e = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, verified)
	budgetSQLState(t, e, "42501")
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET emergency_stop=false WHERE kind='supervisor'`); e != nil {
		t.Fatal(e)
	}
	pending, e := (supervisorfixture.Settlement{DB: s, Incarnation: si}).Reconcile(ctx, verified)
	if e != nil || pending.State != "pending" {
		t.Fatal(pending, e)
	}
	// Real rolled-back transaction with a lost transport confirmation: no accept
	// retry. Only a later explicitly controlled independent delivery follows.
	rollback := supervisorfixture.Settlement{DB: workspaceObservedDB{Pool: s, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "rollback"} }}, Incarnation: si}
	state := releaseState(t, db, h)
	_, e = rollback.Accept(ctx, verified)
	if !errors.Is(e, supervisorfixture.ErrSettlementUnknown) {
		t.Fatal(e)
	}
	if state != releaseState(t, db, h) {
		t.Fatal("unknown rolled-back Commit changed capacity")
	}
	pending, e = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Reconcile(ctx, verified)
	if e != nil || pending.State != "pending" {
		t.Fatal(pending, e)
	}
	// Hold the disabled former Owner row throughout successful settlement: D6
	// recovery acquires no User lock. Pause/PATCH's new epoch must not relabel Run.
	ownerLock, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer ownerLock.Rollback(ctx)
	if _, e = ownerLock.Exec(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, h.u); e != nil {
		t.Fatal(e)
	}
	gate := newWorkspacePause(t)
	defer gate.resume()
	pid := make(chan int, 1)
	first := make(chan error, 1)
	go func() {
		_, err := (supervisorfixture.Settlement{DB: workspaceObservedDB{Pool: s, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx {
			return startCommitGate{Tx: workspaceObservedTx{Tx: tx, commitOutcome: "lost"}, pause: gate}
		}}, Incarnation: si}).Accept(ctx, verified)
		first <- err
	}()
	p1 := <-pid
	select {
	case <-gate.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("accept did not reach real Commit barrier")
	}
	p2, second := budgetAsync(t, s, acceptPhysicalSQL, si, h.w, h.p, h.m, h.r, []byte(stopped.Receipt))
	waitActivationBlock(t, db, p2)
	p3, candidate1 := startSchedulerClaim(t, w, wi, fill[31])
	waitActivationBlock(t, db, p3)
	p4, candidate2 := startSchedulerClaim(t, w2, i2, fill[32])
	waitActivationBlock(t, db, p4)
	schedulerLockReceipt(t, db, "physical-stop-two-consumers-two-last-slot-claimants", p1, p2, p3, p4)
	// Uncommitted release must not appear as spare capacity to another connection.
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 32 {
		t.Fatal("dirty release visible")
	}
	gate.resume()
	if e = <-first; !errors.Is(e, supervisorfixture.ErrSettlementUnknown) {
		t.Fatal("real committed ACK-loss expected", e)
	}
	budgetSQLState(t, <-second, "P0001")
	cr1, cr2 := <-candidate1, <-candidate2
	success := 0
	for _, cr := range []schedulerClaimResult{cr1, cr2} {
		if cr.err == nil {
			success++
		} else {
			budgetSQLState(t, cr.err, "P0001")
		}
	}
	if success != 1 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 32 {
		t.Fatal("last slot double returned or not atomically claimable")
	}
	reconciled, e := (supervisorfixture.Settlement{DB: s, Incarnation: si}).Reconcile(ctx, verified)
	if e != nil || reconciled.State != "consumed" {
		t.Fatal(reconciled, e)
	}
	stable := releaseState(t, db, h)
	_, e = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, verified)
	budgetSQLState(t, e, "P0001")
	if stable != releaseState(t, db, h) {
		t.Fatal("duplicate receipt changed ledger")
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE run_id=$1 AND attempt_state='started' AND configuration_version=$2 AND activation_generation=$3 AND scheduler_epoch=$4 AND physical_release_confirmed_at IS NOT NULL AND http_status IS NULL AND success IS NULL AND failure_code IS NULL AND result_commit_id IS NULL`, h.r, h.v, h.g, h.e) != 1 {
		t.Fatal("physical receipt fabricated a network result or rewrote old lineage")
	}
	if queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind IN ('origin','ip')`) != 0 {
		t.Fatal("retained target concurrency not released")
	}
	if rates != releaseRates(t, db, h) {
		t.Fatal("release refunded tokens, shortened target cooldown or changed original rate state")
	}
	t.Log("physical release committed exactly once; real ACK loss reconciled read-only; concurrent last slot admitted once; no HTTP result/GET/input replay; old Owner lock did not block")
}

func releaseACL(t *testing.T, db, w, c, s *pgxpool.Pool, si, ci string, h schedulerClaimHint) {
	t.Helper()
	ctx := context.Background()
	for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_api", "devpilot_scheduler_controller", "devpilot_scheduler_supervisor"} {
		for _, fn := range []string{"quarantine_physical(uuid,uuid,uuid,uuid,uuid,bigint)", "accept_termination(uuid,uuid,uuid,uuid,uuid,bytea)", "reconcile_termination(uuid,uuid,uuid,uuid,uuid,uuid,bytea)"} {
			var allowed bool
			if e := db.QueryRow(ctx, `SELECT has_function_privilege($1,'scheduler_private.'||$2,'EXECUTE')`, role, fn).Scan(&allowed); e != nil {
				t.Fatal(e)
			}
			want := (role == "devpilot_scheduler_controller" && strings.HasPrefix(fn, "quarantine")) || (role == "devpilot_scheduler_supervisor" && !strings.HasPrefix(fn, "quarantine"))
			if allowed != want {
				t.Fatal("extra release ACL", role, fn)
			}
		}
	}
	for _, p := range []*pgxpool.Pool{w, c} {
		_, e := p.Exec(ctx, acceptPhysicalSQL, si, h.w, h.p, h.m, h.r, []byte(`{}`))
		budgetSQLState(t, e, "42501")
	}
	_, e := s.Exec(ctx, quarantinePhysicalSQL, ci, h.w, h.p, h.m, h.r, 1)
	budgetSQLState(t, e, "42501")
	for _, p := range []*pgxpool.Pool{w, c, s} {
		for _, sql := range []string{`SELECT password_hash FROM users`, `SELECT token_hash FROM sessions`, `UPDATE scheduler_reservations SET reservation_state='released'`, `CREATE TABLE release_forbidden(id int)`} {
			_, e = p.Exec(ctx, sql)
			budgetSQLState(t, e, "42501")
		}
	}
	var raw []byte
	if e = db.QueryRow(ctx, `SELECT jsonb_agg(jsonb_build_object('signature',p.oid::regprocedure::text,'owner',pg_get_userbyid(p.proowner),'config',p.proconfig,'acl',p.proacl)) FROM pg_proc p WHERE p.pronamespace='scheduler_private'::regnamespace AND p.proname IN ('quarantine_physical','accept_termination','reconcile_termination')`).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	t.Log("actual three-function identity/config/ACL " + string(raw))
}
func releaseFaults(t *testing.T, db, s *pgxpool.Pool, si string, h schedulerClaimHint, v supervisorfixture.VerifiedStop) {
	t.Helper()
	ctx := context.Background()
	for _, table := range []string{"scheduler_budget_state", "scheduler_reservations", "monitor_runs", "scheduler_control"} {
		before := releaseState(t, db, h)
		_, e := db.Exec(ctx, `CREATE FUNCTION public.release54_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'release54 injected DML fault' USING ERRCODE='XX000'; END $$; REVOKE ALL ON FUNCTION public.release54_fault() FROM PUBLIC; CREATE TRIGGER release54_fault BEFORE UPDATE ON public.`+table+` FOR EACH ROW EXECUTE FUNCTION public.release54_fault()`)
		if e != nil {
			t.Fatal(e)
		}
		_, e = (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, v)
		budgetSQLState(t, e, "XX000")
		if before != releaseState(t, db, h) {
			t.Fatal("partial release survived injected fault", table)
		}
		if _, e = db.Exec(ctx, `DROP TRIGGER release54_fault ON public.`+table+`; DROP FUNCTION public.release54_fault()`); e != nil {
			t.Fatal(e)
		}
	}
}

func releaseRates(t *testing.T, db *pgxpool.Pool, h schedulerClaimHint) string {
	t.Helper()
	var raw []byte
	e := db.QueryRow(context.Background(), `SELECT jsonb_agg(jsonb_build_object('kind',kind,'key',budget_key,'tokens',tokens,'last_refill',last_refill_at,'not_before',not_before,'target_keep',CASE WHEN kind IN ('origin','ip') THEN keep_until END) ORDER BY kind,budget_key) FROM scheduler_budget_state WHERE kind IN ('global','origin','ip') OR (kind='workspace' AND budget_key=$1) OR (kind='project' AND budget_key=$1||'/'||$2) OR (kind='monitor' AND budget_key=$1||'/'||$2||'/'||$3)`, h.w, h.p, h.m).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func releaseLockStages(t *testing.T, db, s *pgxpool.Pool, si string, h schedulerClaimHint, v supervisorfixture.VerifiedStop) {
	t.Helper()
	ctx := context.Background()
	stages := []struct {
		name, sql string
		args      []any
	}{
		{"Workspace", `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE`, []any{h.w}},
		{"Project", `SELECT id FROM projects WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, []any{h.w, h.p}},
		{"Monitor", `SELECT id FROM http_monitors WHERE workspace_id=$1 AND project_id=$2 AND id=$3 FOR UPDATE`, []any{h.w, h.p, h.m}},
		{"Activation", `SELECT monitor_id FROM monitor_activation_states WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 FOR UPDATE`, []any{h.w, h.p, h.m}},
		{"Schedule", `SELECT monitor_id FROM monitor_schedules WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 FOR UPDATE`, []any{h.w, h.p, h.m}},
		{"Run", `SELECT run_id FROM monitor_runs WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4 FOR UPDATE`, []any{h.w, h.p, h.m, h.r}},
		{"GlobalBudget", `SELECT budget_key FROM scheduler_budget_state WHERE kind='global' FOR UPDATE`, nil},
		{"OriginBudget", `SELECT budget_key FROM scheduler_budget_state WHERE kind='origin' ORDER BY budget_key LIMIT 1 FOR UPDATE`, nil},
		{"IPBudget", `SELECT budget_key FROM scheduler_budget_state WHERE kind='ip' ORDER BY budget_key LIMIT 1 FOR UPDATE`, nil},
		{"Reservation", `SELECT run_id FROM scheduler_reservations WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4 FOR UPDATE`, []any{h.w, h.p, h.m, h.r}},
		{"SupervisorControl", `SELECT control_id FROM scheduler_control WHERE kind='supervisor' AND incarnation=$1 FOR UPDATE`, []any{si}},
	}
	for _, stage := range stages {
		state := releaseState(t, db, h)
		barrier, e := db.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = barrier.Exec(ctx, stage.sql, stage.args...); e != nil {
			t.Fatal(e)
		}
		pid := make(chan int, 1)
		done := make(chan error, 1)
		go func() {
			_, e := (supervisorfixture.Settlement{DB: workspaceObservedDB{Pool: s, pid: pid, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "rollback"} }}, Incarnation: si}).Accept(ctx, v)
			done <- e
		}()
		worker := <-pid
		waitActivationBlock(t, db, worker)
		schedulerLockReceipt(t, db, "physical-release-wait-"+stage.name, worker)
		if e = barrier.Rollback(ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, supervisorfixture.ErrSettlementUnknown) {
			t.Fatal(stage.name, e)
		}
		if state != releaseState(t, db, h) {
			t.Fatal("lock-stage rolled-back release changed ledger", stage.name)
		}
	}
	// Revoke identity while the last control lock is held, after the entry check.
	barrier, e := db.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = barrier.Exec(ctx, `UPDATE scheduler_control SET emergency_stop=true WHERE kind='supervisor'`); e != nil {
		t.Fatal(e)
	}
	pid := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		_, e := (supervisorfixture.Settlement{DB: workspaceObservedDB{Pool: s, pid: pid}, Incarnation: si}).Accept(ctx, v)
		done <- e
	}()
	worker := <-pid
	waitActivationBlock(t, db, worker)
	schedulerLockReceipt(t, db, "physical-release-fresh-identity-after-control-wait", worker)
	if e = barrier.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, <-done, "42501")
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET emergency_stop=false WHERE kind='supervisor'`); e != nil {
		t.Fatal(e)
	}
}
