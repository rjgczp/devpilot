//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

func budgetTargetFixtures(t *testing.T, db *pgxpool.Pool, hosts []string) ([]schedulerClaimHint, []string) {
	var hs []schedulerClaimHint
	var urls []string
	for j, host := range hosts {
		t.Run(fmt.Sprintf("target%d", j), func(t *testing.T) {
			f, m := monitorFixture(t, db)
			raw := "https://" + host + "/private-path-SENTINEL"
			m, err := (monitor.Store{Pool: db}).Update(context.Background(), f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &raw}, monitor.ETag(m))
			if err != nil {
				t.Fatal(err)
			}
			hs = append(hs, budgetHint(t, db, f, m))
			urls = append(urls, raw)
		})
	}
	return hs, urls
}
func budgetCatalog(t *testing.T, hs []schedulerClaimHint, urls []string, addresses []string, keys map[int64][]byte) []scheduler.CatalogEntry {
	t.Helper()
	var fixtures []scheduler.FixtureTarget
	for i, raw := range urls {
		fixtures = append(fixtures, scheduler.FixtureTarget{URL: raw, Addresses: []netip.Addr{netip.MustParseAddr(addresses[i])}})
	}
	d, err := scheduler.NewTargetDirectory(keys, fixtures)
	if err != nil {
		t.Fatal(err)
	}
	var entries []scheduler.CatalogEntry
	for i, h := range hs {
		e, err := d.Bind(checkpointTwoHint(h), urls[i], netip.MustParseAddr(addresses[i]))
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
	return entries
}
func budgetRotate(t *testing.T, p *pgxpool.Pool, inc string, g int64, es []scheduler.CatalogEntry) error {
	t.Helper()
	raw, err := json.Marshal(es)
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Exec(context.Background(), `SELECT scheduler_private.rotate_target_budget_keys($1::uuid,$2::bigint,$3::jsonb)`, inc, g, raw)
	return err
}

const budgetReserveSQL = `SELECT scheduler_private.reserve_target($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,1,$8::bigint,$9::text,$10::text)`

func budgetReserve(p *pgxpool.Pool, inc string, h schedulerClaimHint, k scheduler.TargetDigest) error {
	var raw []byte
	return p.QueryRow(context.Background(), budgetReserveSQL, inc, h.w, h.p, h.m, h.u, h.b, h.r, k.Generation, k.OriginKey, k.IPKey).Scan(&raw)
}
func budgetStarted(t *testing.T, db, w, s *pgxpool.Pool, wi, si string, h schedulerClaimHint) scheduler.Lease {
	t.Helper()
	lease, err := (scheduler.Store{DB: w, Incarnation: wi}).Claim(context.Background(), checkpointTwoHint(h))
	if err != nil {
		t.Fatal(err)
	}
	ns := budgetPrepare(t, db, s, si, wi, h)
	if _, err = schedulerStart(t, w, wi, h); err != nil {
		t.Fatal(err)
	}
	budgetConsume(t, s, si, ns, h)
	return lease
}
func TestBudgetTargetCrossTenant(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com", "a.fixture.example.com", "b.fixture.example.com", "c.fixture.example.com"})
	ips := []string{"192.0.2.10", "192.0.2.10", "192.0.2.10", "192.0.2.10"}
	es := budgetCatalog(t, hs, urls, ips, map[int64][]byte{1: []byte(strings.Repeat("k", 32))})
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	// All actual starts consume their own independent tenant budget. No target TCP
	// or Supervisor physical termination is performed in this database checkpoint.
	budgetStarted(t, db, w1, s, i1, si, hs[0])
	budgetStarted(t, db, w2, s, i2, si, hs[1])
	budgetStarted(t, db, w2, s, i2, si, hs[2])
	budgetStarted(t, db, w1, s, i1, si, hs[3])
	budgetSQLState(t, budgetReserve(w1, i1, hs[0], es[0].Keys[0]), "00000")
	budgetSQLState(t, budgetReserve(w2, i2, hs[1], es[1].Keys[0]), "P0001") // same origin across tenants
	budgetSQLState(t, budgetReserve(w2, i2, hs[2], es[2].Keys[0]), "P0001") // alias, same IP, within1s
	if queryInt(t, db, `SELECT count(*) FROM scheduler_budget_state WHERE kind='origin' AND in_use>0`) != 1 {
		t.Fatal("failed IP stage leaked origin slot")
	}
	time.Sleep(time.Second + time.Millisecond)
	budgetSQLState(t, budgetReserve(w2, i2, hs[2], es[2].Keys[0]), "00000") // IP second concurrent slot
	time.Sleep(time.Second + time.Millisecond)
	budgetSQLState(t, budgetReserve(w1, i1, hs[3], es[3].Keys[0]), "P0001") // IP already2, despite rate refilled
	if queryInt(t, db, `SELECT max(in_use) FROM scheduler_budget_state WHERE kind='origin'`) != 1 || queryInt(t, db, `SELECT max(in_use) FROM scheduler_budget_state WHERE kind='ip'`) != 2 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 2 {
		t.Fatal("origin1/IP2 cap violated")
	}
	budgetSQLState(t, budgetReserve(w1, i1, hs[0], es[0].Keys[0]), "P0001") // once-only
	if err := budgetRotate(t, c, ci, 2, budgetCatalog(t, hs, urls, ips, map[int64][]byte{1: []byte(strings.Repeat("k", 32)), 2: []byte(strings.Repeat("n", 32))})); err == nil {
		t.Fatal("rotated active/quarantined slots")
	}
	var raw string
	if err := db.QueryRow(context.Background(), `SELECT jsonb_build_object('runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r),'reservations',(SELECT jsonb_agg(to_jsonb(q)) FROM scheduler_reservations q),'catalog',(SELECT target_catalog FROM scheduler_control WHERE kind='deployment'))::text`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture.example.com", "192.0.2.10", "private-path-SENTINEL", strings.Repeat("k", 32)} {
		if strings.Contains(raw, secret) {
			t.Fatal("persisted raw target/secret")
		}
	}
}
func TestBudgetTargetRotationPreservesCooling(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	ips := []string{"2001:db8::10"}
	one := map[int64][]byte{1: []byte(strings.Repeat("k", 32))}
	two := map[int64][]byte{1: one[1], 2: []byte(strings.Repeat("n", 32))}
	e1 := budgetCatalog(t, hs, urls, ips, one)
	e2 := budgetCatalog(t, hs, urls, ips, two)
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, e1), "00000")
	// A cold historical token fixture has no Run or physical permission. This is
	// not a forged termination receipt. Rotation must preserve its exact cooling.
	if _, err := db.Exec(context.Background(), `UPDATE scheduler_budget_state SET tokens=0,not_before=clock_timestamp()+interval '60 seconds' WHERE kind='origin' AND budget_key=$1`, e1[0].Keys[0].OriginKey); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	if err := db.QueryRow(context.Background(), `SELECT not_before FROM scheduler_budget_state WHERE kind='origin' AND budget_key=$1`, e1[0].Keys[0].OriginKey).Scan(&before); err != nil {
		t.Fatal(err)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 2, e2), "00000")
	var after, keep time.Time
	var tokens int64
	if err := db.QueryRow(context.Background(), `SELECT not_before,keep_until,tokens FROM scheduler_budget_state WHERE kind='origin' AND budget_key=$1`, e1[0].Keys[0].OriginKey).Scan(&after, &keep, &tokens); err != nil {
		t.Fatal(err)
	}
	if !after.Equal(before) || tokens != 0 || keep.Before(time.Now().Add(3590*time.Second)) {
		t.Fatal("rotation reset cooling/retention")
	}
	malformed := budgetCatalog(t, hs, urls, ips, map[int64][]byte{2: two[2], 3: []byte(strings.Repeat("q", 32))})
	budgetSQLState(t, budgetRotate(t, c, ci, 3, malformed), "P0001")
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	budgetStarted(t, db, w, s, i, si, hs[0])
	budgetSQLState(t, budgetReserve(w, i, hs[0], e2[0].Keys[1]), "P0001")
	if queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind IN ('origin','ip')`) != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 0 {
		t.Fatal("new generation bypassed retained cooldown or leaked slots")
	}
}
func TestBudgetTargetUnknownCommitAndACL(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	es := budgetCatalog(t, hs, urls, []string{"192.0.2.10"}, map[int64][]byte{1: []byte(strings.Repeat("k", 32))})
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	budgetSQLState(t, budgetReserve(w, i, hs[0], es[0].Keys[0]), "P0001")
	lease := budgetStarted(t, db, w, s, i, si, hs[0])
	budgetSQLState(t, budgetReserve(w2, i2, hs[0], es[0].Keys[0]), "P0001")
	bad := hs[0]
	bad.p = bad.w
	budgetSQLState(t, budgetReserve(w, i, bad, es[0].Keys[0]), "P0001")
	store := scheduler.Store{DB: workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: "lost"} }}, Incarnation: i}
	permit, err := store.ReserveTarget(context.Background(), checkpointTwoHint(hs[0]), lease, es[0].Keys[0])
	if !errors.Is(err, scheduler.ErrCommitUnknown) || permit.PermitID != "" {
		t.Fatal("unknown target commit exposed permission", err, permit)
	}
	budgetSQLState(t, budgetReserve(w, i, hs[0], es[0].Keys[0]), "P0001")
	if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 1 {
		t.Fatal("uncertain ACK minted another permit")
	}
	for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_controller", "devpilot_scheduler_api", "devpilot_scheduler_supervisor"} {
		var reserve, rotate bool
		if err := db.QueryRow(context.Background(), `SELECT has_function_privilege($1,'scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)','EXECUTE'),has_function_privilege($1,'scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb)','EXECUTE')`, role).Scan(&reserve, &rotate); err != nil || reserve != (role == "devpilot_scheduler_worker") || rotate != (role == "devpilot_scheduler_controller") {
			t.Fatal("ACL diff escaped approval", role, err)
		}
	}
	for _, p := range []*pgxpool.Pool{w, c} {
		for _, sql := range []string{`SELECT password_hash FROM users`, `SELECT token_hash FROM sessions`, `UPDATE scheduler_reservations SET termination_receipt=decode(repeat('ff',32),'hex')`, `UPDATE scheduler_budget_state SET in_use=0`, `UPDATE scheduler_control SET gate='fixture'`, `SET ROLE devpilot_scheduler_owner`} {
			_, err := p.Exec(context.Background(), sql)
			budgetSQLState(t, err, "42501")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='scheduler_private' AND p.prosecdef AND (p.proconfig IS NULL OR NOT 'search_path=pg_catalog, scheduler_private'=ANY(p.proconfig) OR EXISTS(SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`) != 0 {
		t.Fatal("unsafe function config")
	}
}

func TestBudgetTargetConcurrentLastSlot(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com", "a.fixture.example.com"})
	es := budgetCatalog(t, hs, urls, []string{"192.0.2.10", "192.0.2.10"}, map[int64][]byte{1: []byte(strings.Repeat("k", 32))})
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	w1, i1 := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	w2, i2 := schedulerWorkerLogin(t, db, "scheduler_claim_worker2")
	s, si := schedulerSupervisorLogin(t, db)
	budgetStarted(t, db, w1, s, i1, si, hs[0])
	budgetStarted(t, db, w2, s, i2, si, hs[1])
	barrier, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(context.Background())
	if _, err = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='origin' AND budget_key=$1 FOR UPDATE`, es[0].Keys[0].OriginKey); err != nil {
		t.Fatal(err)
	}
	x, k := hs[0], es[0].Keys[0]
	p1, ch1 := budgetAsync(t, w1, budgetReserveSQL, i1, x.w, x.p, x.m, x.u, x.b, x.r, k.Generation, k.OriginKey, k.IPKey)
	waitActivationBlock(t, db, p1)
	checkpointTwoProbe(t, db, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, true, x.u)
	checkpointTwoProbe(t, db, `SELECT id FROM workspaces WHERE id=$1 FOR NO KEY UPDATE NOWAIT`, true, x.w)
	checkpointTwoProbe(t, db, `SELECT reservation_id FROM scheduler_reservations WHERE run_id=$1 FOR UPDATE NOWAIT`, false, x.r)
	x, k = hs[1], es[1].Keys[0]
	p2, ch2 := budgetAsync(t, w2, budgetReserveSQL, i2, x.w, x.p, x.m, x.u, x.b, x.r, k.Generation, k.OriginKey, k.IPKey)
	waitActivationBlock(t, db, p2)
	schedulerLockReceipt(t, db, "target-two-tenants-origin-last-slot", p1, p2)
	if err = barrier.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	budgetSQLState(t, <-ch1, "00000")
	budgetSQLState(t, <-ch2, "P0001")
	if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 1 || queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind='origin'`) != 1 || queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind='ip'`) != 1 {
		t.Fatal("target race oversubscribed or failed rollback")
	}
}

func TestBudgetTargetRealOriginCooldown(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	es := budgetCatalog(t, hs, urls, []string{"192.0.2.10"}, map[int64][]byte{1: []byte(strings.Repeat("k", 32))})
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	// A cold historical bucket, with no Run/namespace/receipt. Observe sixty
	// seconds of REAL DB time; no physical slot is released to run this test.
	var deadline time.Time
	if err := db.QueryRow(context.Background(), `UPDATE scheduler_budget_state SET tokens=0,last_refill_at=clock_timestamp(),not_before=clock_timestamp()+interval '60 seconds' WHERE kind='origin' AND budget_key=$1 RETURNING not_before`, es[0].Keys[0].OriginKey).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	wait := func(until time.Time) {
		var n time.Time
		if err := db.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if until.After(n) {
			time.Sleep(until.Sub(n))
		}
	}
	wait(deadline.Add(-23 * time.Second))
	budgetStarted(t, db, w, s, i, si, hs[0])
	budgetSQLState(t, budgetReserve(w, i, hs[0], es[0].Keys[0]), "P0001")
	if queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind IN ('origin','ip')`) != 0 {
		t.Fatal("early refusal leaked capacity")
	}
	wait(deadline.Add(time.Millisecond))
	budgetSQLState(t, budgetReserve(w, i, hs[0], es[0].Keys[0]), "00000")
	t.Logf("real origin60s deadline=%s; before deadline refused, after deadline single reservation committed", deadline.Format(time.RFC3339Nano))
}
func TestBudgetTargetFinalSupervisorGate(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	es := budgetCatalog(t, hs, urls, []string{"192.0.2.10"}, map[int64][]byte{1: []byte(strings.Repeat("k", 32))})
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	w, i := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	budgetStarted(t, db, w, s, i, si, hs[0])
	barrier, err := db.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(context.Background())
	if _, err = barrier.Exec(context.Background(), `UPDATE scheduler_control SET emergency_stop=true WHERE kind='supervisor' AND incarnation=$1`, si); err != nil {
		t.Fatal(err)
	}
	h, k := hs[0], es[0].Keys[0]
	pid, ch := budgetAsync(t, w, budgetReserveSQL, i, h.w, h.p, h.m, h.u, h.b, h.r, k.Generation, k.OriginKey, k.IPKey)
	waitActivationBlock(t, db, pid)
	schedulerLockReceipt(t, db, "target-final-supervisor-stop", pid)
	if err = barrier.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	budgetSQLState(t, <-ch, "P0001")
	if queryInt(t, db, `SELECT sum(in_use) FROM scheduler_budget_state WHERE kind IN ('origin','ip')`) != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 0 {
		t.Fatal("pre-wait supervisor hint bypassed fresh gate")
	}
	// Owner and Member projections expose no hash or other-tenant quota state.
	rows, err := db.Query(context.Background(), `SELECT x.data FROM workspace_members b JOIN sessions se ON se.user_id=b.user_id CROSS JOIN LATERAL scheduler_private.execution_status(se.token_hash,$1::uuid,$2::uuid,$3::uuid) x WHERE b.workspace_id=$1::uuid`, h.w, h.p, h.m)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		count++
		for _, sentinel := range []string{"origin_key", "ip_key", "target_keys", k.IPKey, k.OriginKey} {
			if strings.Contains(string(raw), sentinel) {
				t.Fatal("user projection leaked target budget")
			}
		}
	}
	if err = rows.Err(); err != nil || count != 2 {
		t.Fatal("Owner/Member actual status projections", err, count)
	}
}
