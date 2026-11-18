//go:build integration && schedulerfixture && privateworker && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture && privateloopfixture && capacityfixture && privateservicefixture

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// The coordinator provisions only a disposable cluster and observes public API
// and ledger facts. Every execution/stop/recovery call is made by the new service
// binaries, never by this test. The unchanged approved candidates are hash-bound.
func TestPrivateServicePG(t *testing.T) { privateServicePG(t, nil) }

type privateServiceOptions struct {
	URL, Mode string
	Refusal   bool
	Apply     func(*testing.T, *pgxpool.Pool)
}

func privateServicePG(t *testing.T, options *privateServiceOptions) {
	ctx := context.Background()
	db := openDB(t)
	capacitySchema(t, db)
	if options != nil {
		options.Apply(t, db)
	}
	if _, e := db.Exec(ctx, `REVOKE TEMP ON DATABASE devpilot_registration_test FROM PUBLIC`); e != nil {
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
	p := &periodicPipe{t: t, in: periodicScanner(), prefix: "NETWORK64 "}
	key := p.exchange("service-key", nil)
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET stop_public_key=$1 WHERE kind='supervisor' AND incarnation=$2`, key.Frame, si); e != nil {
		t.Fatal(e)
	}
	var topology bool
	if e := db.QueryRow(ctx, `SELECT current_setting('data_directory')='/var/lib/postgresql/18/docker' AND current_setting('temp_tablespaces')='' AND current_setting('archive_mode')='off' AND current_setting('logging_collector')='off' AND current_setting('log_destination')='stderr' AND NOT EXISTS(SELECT 1 FROM pg_tablespace WHERE pg_tablespace_location(oid)<>'') AND NOT EXISTS(SELECT 1 FROM pg_replication_slots)`).Scan(&topology); e != nil || !topology {
		t.Fatal("unsupported topology", e)
	}
	binding := p.exchange("capacity-filesystems", nil)
	r, ri := capacityReporter(t, db, string(binding.Frame))
	r = periodicPrivateLogin(t, db, r)
	f, m := monitorFixture(t, db)
	url := "https://a.fixture.example.com/fixture/private-path-SENTINEL"
	if options != nil {
		url = options.URL
	}
	interval, timeout := int64(60), int64(10)
	var e error
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &url, IntervalSeconds: &interval, TimeoutSeconds: &timeout}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	mode := "private-isolated"
	if options != nil {
		mode = options.Mode
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET execution_mode=$1`, mode); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, `UPDATE scheduler_control SET gate='`+mode+`',emergency_stop=false,verified_at=clock_timestamp(),expires_at=clock_timestamp()+interval '1 hour',manifest_digest=decode(repeat('ab',32),'hex') WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	server, client, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	a, h, _ := checkpointProjectionGet(t, client, server.URL, status75Base(m), f.ownerToken)
	code, _, raw := checkpointHTTP(t, client, server.URL, "PUT", status75Base(m)+"/execution-authorization", fmt.Sprintf(`{"configuration_version":%q,"consent_profile":"periodic-http-get-v1","authorization_seconds":900}`, a.ConfigurationVersion), f.ownerToken, h.Get("ETag"), nil)
	if code != 200 {
		t.Fatal("real Owner authorization", code, string(raw))
	}
	ip := netip.MustParseAddr("93.184.216.34")
	directory, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32))}, []scheduler.FixtureTarget{{URL: m.URL, Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := directory.Bind(scheduler.Hint{WorkspaceID: m.WorkspaceID, ProjectID: m.ProjectID, MonitorID: m.ID, ConfigurationVersion: m.Version}, m.URL, ip)
	if e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	profile := func(pool *pgxpool.Pool, inc string) map[string]string {
		cc := pool.Config().ConnConfig
		return map[string]string{"dsn": fmt.Sprintf("postgres://%s:%s@pg:5432/devpilot_registration_test?sslmode=disable", cc.User, cc.Password), "incarnation": inc}
	}
	p.exchange("service-config", map[string]any{"workers": []any{profile(w1, i1), profile(w2, i2)}, "supervisor": profile(s, si), "controller": profile(c, ci), "reporter": profile(r, ri), "url": m.URL, "scope": map[string]string{"workspace_id": m.WorkspaceID, "project_id": m.ProjectID, "monitor_id": m.ID, "configuration_version": m.Version}})
	wait := func(name string, seconds int, query string, args ...any) {
		t.Helper()
		for end := time.Now().Add(time.Duration(seconds) * time.Second); time.Now().Before(end); time.Sleep(200 * time.Millisecond) {
			var ok bool
			if e := db.QueryRow(ctx, query, args...).Scan(&ok); e != nil {
				t.Fatal(name, e)
			}
			if ok {
				return
			}
		}
		t.Fatal("bounded wait: " + name)
	}
	history := func(count int, latest string) {
		t.Helper()
		code, _, body := checkpointHTTP(t, client, server.URL, "GET", status75Base(m)+"/history", "", f.ownerToken, "", nil)
		var page monitor.HistoryPage
		if code != 200 || json.Unmarshal(body, &page) != nil || len(page.Data) != count || page.Data[0].Status != latest {
			t.Fatal("real history", code, string(body))
		}
	}
	snapshot := func(label string) {
		t.Helper()
		var raw []byte
		if e := db.QueryRow(ctx, `SELECT jsonb_build_object('label',$1::text,'at',clock_timestamp(),'runs',(SELECT jsonb_agg(jsonb_build_object('run_id',run_id,'state',attempt_state,'started_at',started_at,'finished_at',finished_at,'released_at',physical_release_confirmed_at,'http_status',http_status,'success',success) ORDER BY claimed_at) FROM monitor_runs),'due',(SELECT next_due_at FROM monitor_schedules WHERE monitor_id=$2),'in_use',(SELECT in_use FROM scheduler_budget_state WHERE kind='global'))`, label, m.ID).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		p.exchange("service-snapshot", json.RawMessage(raw))
	}
	if options != nil && options.Refusal {
		reply := p.exchange("service-refusal-start", nil)
		kind := string(reply.Frame)
		defer p.exchange("service-stop", nil)
		if kind == "unapproved" || kind == "approval-expired" {
			time.Sleep(3 * time.Second)
			if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 0 {
				t.Fatal("unapproved/expired target claimed")
			}
		} else {
			state, status := "unknown", "unknown"
			if kind == "private4" || kind == "private6" {
				state, status = "completed", "failure"
			}
			wait("refusal and trusted physical release", 45, `SELECT count(*)=1 FROM monitor_runs WHERE attempt_state=$1 AND physical_release_confirmed_at IS NOT NULL`, state)
			if state == "completed" && queryInt(t, db, `SELECT count(*) FROM monitor_runs WHERE success=false AND http_status IS NULL AND failure_code='blocked_target'`) != 1 {
				t.Fatal("DNS safety refusal misclassified")
			}
			if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 {
				t.Fatal("refusal not settled after physical proof")
			}
			history(1, status)
			snapshot("refused-" + kind)
		}
		p.exchange("service-refusal-assert", nil)
		return
	}
	p.exchange("service-start", nil)
	defer p.exchange("service-stop", nil)
	wait("first trusted Result", 45, `SELECT count(*)=1 FROM monitor_runs WHERE attempt_state='completed' AND success AND http_status=200 AND physical_release_confirmed_at IS NOT NULL`)
	history(1, "success")
	snapshot("first")
	p.exchange("service-stop", nil)
	p.exchange("service-start", nil)
	time.Sleep(2200 * time.Millisecond)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 1 {
		t.Fatal("restart replayed or claimed before due")
	}
	p.exchange("service-hold", nil)
	wait("second real Run", 100, `SELECT count(*)=1 FROM monitor_runs WHERE attempt_state='started'`)
	var lost string
	if e := db.QueryRow(ctx, `SELECT run_id::text FROM monitor_runs WHERE attempt_state='started'`).Scan(&lost); e != nil {
		t.Fatal(e)
	}
	p.exchange("service-lose-supervisor", map[string]string{"run_id": lost})
	wait("unknown without trusted acceptance", 10, `SELECT EXISTS(SELECT 1 FROM monitor_runs r JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.run_id=$1 AND r.attempt_state='unknown' AND r.physical_release_confirmed_at IS NULL AND q.reservation_state='quarantined')`, lost)
	if queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 1 {
		t.Fatal("untrusted release returned slot")
	}
	pair := status75ReadPair(t, client, server.URL, m, f.ownerToken)
	if pair.s.CurrentObservation != nil {
		t.Fatal("old success replaced new unknown")
	}
	history(2, "unknown")
	snapshot("lost-unaccepted")
	p.exchange("service-start", nil)
	wait("restart verifies immutable original physical proof", 10, `SELECT EXISTS(SELECT 1 FROM monitor_runs WHERE run_id=$1 AND attempt_state='unknown' AND physical_release_confirmed_at IS NOT NULL)`, lost)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 2 {
		t.Fatal("unknown GET replay")
	}
	wait("future trusted Result", 100, `SELECT count(*)=2 FROM monitor_runs WHERE attempt_state='completed' AND success AND http_status=200 AND physical_release_confirmed_at IS NOT NULL`)
	history(3, "success")
	snapshot("recovered")
	p.exchange("service-lose-reporter", nil)
	var admitted bool
	if e := db.QueryRow(ctx, `SELECT scheduler_private.storage_admission(clock_timestamp())`).Scan(&admitted); e != nil || admitted {
		t.Fatal("storage unavailable did not close admission", e)
	}
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 3 {
		t.Fatal("Reporter loss admitted new work")
	}
	pair = status75ReadPair(t, client, server.URL, m, f.ownerToken)
	code, _, raw = checkpointHTTP(t, client, server.URL, "PUT", status75Base(m)+"/activation", `{"desired_state":"disabled","configuration_version":"`+m.Version+`"}`, f.ownerToken, pair.tag, nil)
	if code != 200 {
		t.Fatal("unavailable storage pause", code, string(raw))
	}
	name := "private-service-stopped"
	if _, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{Name: &name}, monitor.ETag(m)); e != nil {
		t.Fatal("unavailable storage PATCH", e)
	}
	snapshot("reporter-closed-pause-patch")
	p.exchange("service-assert", map[string]int{"gets": 3, "physical_acceptances": 3})
}
