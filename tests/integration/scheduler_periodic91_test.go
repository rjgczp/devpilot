//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture && releasefixture && jointfixture && periodicfixture

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

// This protocol exists only in the disposable test binary. Two separate Docker
// Workers receive only their own LOGIN. The coordinator keeps Supervisor DB
// credentials; the original external Supervisor alone owns physical proofs.
type periodicMessage struct {
	Op      string          `json:"op"`
	Payload json.RawMessage `json:"payload"`
}
type periodicReply struct {
	jointReply
	Worker  periodicMessage   `json:"worker"`
	Workers []periodicMessage `json:"workers"`
}
type periodicPipe struct {
	t      *testing.T
	in     *bufio.Scanner
	prefix string
	pool   *pgxpool.Pool
}

func (p *periodicPipe) exchange(op string, payload any) periodicReply {
	p.t.Helper()
	if p.pool != nil && p.pool.Stat().AcquiredConns() != 0 {
		p.t.Fatal("network handoff retained DB connection")
	}
	raw, e := json.Marshal(map[string]any{"op": op, "payload": payload})
	if e != nil {
		p.t.Fatal(e)
	}
	fmt.Println(p.prefix + string(raw))
	if !p.in.Scan() {
		p.t.Fatal("private management pipe lost")
	}
	var r periodicReply
	if json.Unmarshal(p.in.Bytes(), &r) != nil {
		p.t.Fatal("invalid management reply")
	}
	return r
}
func periodicScanner() *bufio.Scanner {
	s := bufio.NewScanner(os.Stdin)
	s.Buffer(make([]byte, 4096), 65536)
	return s
}
func periodicDecode(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if json.Unmarshal(raw, out) != nil {
		t.Fatal("invalid fixture message")
	}
}

type periodicBoot struct{ DSN, Incarnation, IP string }
type periodicTick struct {
	Hint         scheduler.Hint
	Lease        *scheduler.Lease
	PID, Backend int
	Login        string
}
type periodicExecution struct{ Line handoff.Lineage }

// Only consume_input can cross this adapter, and only its Commit sends a
// request. The coordinator executes the original Supervisor transaction after
// checking the complete active lineage. No generic SQL or Supervisor password
// is available in a Worker, and a lost acknowledgement exposes no input.
const periodicConsumeSQL = `SELECT scheduler_private.consume_input($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bigint)`

type periodicInputDB struct {
	scheduler.Database
	pipe *periodicPipe
	line handoff.Lineage
}
type periodicInputTx struct {
	pgx.Tx
	db     periodicInputDB
	staged bool
	closed bool
}

func (d periodicInputDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if ctx.Err() != nil || opts.IsoLevel != pgx.ReadCommitted {
		return nil, scheduler.ErrUnavailable
	}
	return &periodicInputTx{db: d}, nil
}
func (x *periodicInputTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	l := x.db.line
	want := []any{l.SupervisorIncarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, l.NamespaceIncarnation, l.FencingToken}
	a, _ := json.Marshal(args)
	b, _ := json.Marshal(want)
	if ctx.Err() != nil || x.closed || x.staged || sql != periodicConsumeSQL || string(a) != string(b) {
		return pgconn.CommandTag{}, scheduler.ErrUnavailable
	}
	x.staged = true
	return pgconn.NewCommandTag("SELECT 1"), nil
}
func (x *periodicInputTx) Commit(ctx context.Context) error {
	if ctx.Err() != nil || x.closed || !x.staged {
		return scheduler.ErrUnavailable
	}
	x.closed = true
	if x.db.pipe.exchange("consume-input", x.db.line).Event != "input_committed" || ctx.Err() != nil {
		return scheduler.ErrCommitUnknown
	}
	return nil
}
func (x *periodicInputTx) Rollback(context.Context) error { x.closed = true; return nil }

type periodicTarget struct{ pipe *periodicPipe }

func (b periodicTarget) InstallTarget(ctx context.Context, r targetpermit.Request, p scheduler.TargetPermit) (targetpermit.Confirmation, error) {
	reply := b.pipe.exchange("install-joint", supervisorfixture.Install{Request: r, PermitID: p.PermitID, CommittedAt: p.CommittedAt, PermitDeadline: p.PermitDeadline})
	if ctx.Err() != nil || !reply.Confirmation.Matches(r) {
		return targetpermit.Confirmation{}, targetpermit.ErrDenied
	}
	return reply.Confirmation, nil
}

// The Worker is a separate container, never a subtest in the coordinator.
func TestPeriodicWorkerProcess(t *testing.T) {
	if os.Getenv("DEVPILOT_PERIODIC_WORKER") != "fixture-only" {
		t.Fatal("requires private disposable Worker launcher")
	}
	pipe := &periodicPipe{t: t, in: periodicScanner(), prefix: "PERIODIC "}
	if !pipe.in.Scan() {
		t.Fatal("missing private bootstrap")
	}
	var boot periodicBoot
	periodicDecode(t, pipe.in.Bytes(), &boot)
	cfg, e := pgxpool.ParseConfig(boot.DSN)
	if e != nil {
		t.Fatal("invalid fixture connection")
	}
	cfg.MaxConns = 1
	if cfg.ConnConfig.Database != "devpilot_registration_test" || (cfg.ConnConfig.User != "scheduler_claim_worker1" && cfg.ConnConfig.User != "scheduler_claim_worker2") {
		t.Fatal("unguarded Worker identity")
	}
	pool, e := pgxpool.NewWithConfig(context.Background(), cfg)
	if e != nil {
		t.Fatal("fixture connection unavailable")
	}
	defer pool.Close()
	pipe.pool = pool
	ctx := context.Background()
	var cluster, version, login, statement, transaction, lock string
	var backend int
	if e = pool.QueryRow(ctx, `SELECT current_setting('cluster_name'),current_setting('server_version_num'),session_user::text,pg_backend_pid(),current_setting('statement_timeout'),current_setting('transaction_timeout'),current_setting('lock_timeout')`).Scan(&cluster, &version, &login, &backend, &statement, &transaction, &lock); e != nil || cluster != "devpilot-budget-disposable" || !strings.HasPrefix(version, "18") || login != cfg.ConnConfig.User || statement != "2s" || transaction != "2s" || lock != "250ms" {
		t.Fatal("Worker environment/transaction guard", e)
	}
	for _, sql := range []string{`SELECT * FROM public.sessions LIMIT 0`, `SELECT * FROM public.monitor_runs LIMIT 0`, periodicConsumeSQL} {
		var err error
		if sql == periodicConsumeSQL {
			_, err = pool.Exec(ctx, sql, boot.Incarnation, boot.Incarnation, boot.Incarnation, boot.Incarnation, boot.Incarnation, boot.Incarnation, "1")
		} else {
			_, err = pool.Exec(ctx, sql)
		}
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "42501" {
			t.Fatal("Worker privilege boundary changed")
		}
	}
	periodicEmit(t, "ready", map[string]any{"pid": os.Getpid(), "backend": backend, "login": login})
	store := scheduler.Store{DB: pool, Incarnation: boot.Incarnation}
	worker := scheduler.NewFixtureWorker(store)
	var claimed periodicTick
	for {
		if !pipe.in.Scan() {
			t.Fatal("Worker command channel lost")
		}
		var cmd periodicMessage
		periodicDecode(t, pipe.in.Bytes(), &cmd)
		switch cmd.Op {
		case "tick":
			v := periodicTick{PID: os.Getpid(), Backend: backend, Login: login}
			hs, err := worker.Candidates(ctx, "")
			if err != nil {
				t.Fatal(err)
			}
			if len(hs) > 1 {
				t.Fatal("unexpected scope in dedicated single-Monitor cluster")
			}
			if len(hs) == 1 {
				v.Hint = hs[0]
				lease, err := worker.Claim(ctx, hs[0])
				if err == nil {
					v.Lease = &lease
					claimed = v
				} else if !errors.Is(err, scheduler.ErrCandidate) {
					t.Fatal(err)
				}
			}
			periodicEmit(t, "tick", v)
		case "execute":
			var exec periodicExecution
			periodicDecode(t, cmd.Payload, &exec)
			if claimed.Lease == nil || exec.Line.RunID != claimed.Lease.RunID {
				t.Fatal("execution without confirmed Claim")
			}
			periodicExecute(t, pipe, store, worker, claimed, exec.Line, boot.IP)
			claimed = periodicTick{}
			periodicEmit(t, "done", nil)
		case "stop":
			periodicEmit(t, "stopped", nil)
			return
		default:
			t.Fatal("unsupported Worker command")
		}
	}
}
func periodicEmit(t *testing.T, op string, payload any) {
	t.Helper()
	raw, e := json.Marshal(map[string]any{"op": op, "payload": payload})
	if e != nil {
		t.Fatal(e)
	}
	fmt.Println("PERIODIC " + string(raw))
}
func periodicExecute(t *testing.T, p *periodicPipe, store scheduler.Store, worker *scheduler.Worker, v periodicTick, line handoff.Lineage, address string) {
	ctx := context.Background()
	snapshot, e := worker.Start(ctx, v.Hint, *v.Lease)
	if e != nil {
		t.Fatal("confirmed Start required", e)
	}
	pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: periodicInputDB{pipe: p, line: line}, Incarnation: line.SupervisorIncarnation})
	if e != nil {
		t.Fatal("confirmed independent input required", e)
	}
	frame, e := io.ReadAll(pipes.Envelope)
	if e != nil {
		t.Fatal(e)
	}
	ack, e := io.ReadAll(pipes.Receipt)
	pipes.Close()
	if e != nil {
		t.Fatal(e)
	}
	launch := map[string][]byte{"envelope": frame, "receipt": ack}
	if p.exchange("launch", launch).Event != "sealed" {
		t.Fatal("Probe boundary not sealed")
	}
	next := p.exchange("next", nil)
	if next.Event != "target_request" {
		t.Fatal("full DNS validation missing")
	}
	ip, e := netip.ParseAddr(address)
	if e != nil {
		t.Fatal(e)
	}
	dir, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte(strings.Repeat("k", 32))}, []scheduler.FixtureTarget{{URL: "https://a.fixture.example.com/fixture/private-path-SENTINEL", Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	controller := snapshot.TargetController(store, v.Hint, dir, periodicTarget{p})
	reply := jointHandle(t, next.Frame, func(ctx context.Context, r targetpermit.Request) (targetpermit.Confirmation, error) {
		if r.Lineage != line || r.IP != ip || r.Host != "a.fixture.example.com" || r.Port != 443 {
			t.Fatal("unexpected target lineage")
		}
		return controller.Authorize(ctx, r)
	})
	if len(reply) == 0 || p.exchange("reply", map[string][]byte{"frame": reply}).Event != "reply_delivered" {
		t.Fatal("target permission unconfirmed")
	}
	next = p.exchange("next", nil)
	if next.Result == nil || !next.Result.Success || next.Result.HTTPStatus == nil || *next.Result.HTTPStatus != 200 {
		t.Fatal("actual Header-only GET failed")
	}
	if p.exchange("replay", launch).Event != "replay_rejected" {
		t.Fatal("same Run input replay accepted")
	}
	if p.exchange("finish", nil).Event != "physically_stopped" {
		t.Fatal("independent termination unconfirmed")
	}
	result, e := scheduler.NetworkResult(line, next.Result)
	if e != nil {
		t.Fatal(e)
	}
	commit, e := scheduler.NewResultCommit(result)
	if e != nil {
		t.Fatal(e)
	}
	if e = worker.CommitResult(ctx, commit); e != nil {
		t.Fatal("Result Commit not confirmed", e)
	}
	if ok, e := worker.ReconcileResult(ctx, commit); e != nil || !ok {
		t.Fatal("fixed Result identity not observed", e)
	}
}

// Existing fixture LOGIN memberships/ACLs are reused verbatim. Give every
// process a different ephemeral password instead of sharing historical test
// constants across the newly separated containers.
func periodicPrivateLogin(t *testing.T, admin, old *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	cfg := old.Config()
	var password string
	if e := admin.QueryRow(ctx, `SELECT uuidv7()::text || uuidv7()::text`).Scan(&password); e != nil {
		t.Fatal(e)
	}
	if _, e := admin.Exec(ctx, `ALTER ROLE `+pgx.Identifier{cfg.ConnConfig.User}.Sanitize()+` PASSWORD '`+password+`'`); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Password = password
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestPeriodicSchedulerPG(t *testing.T) { runPeriodicSchedulerPG(t, false) }

func runPeriodicSchedulerPG(t *testing.T, privateRuntime bool) {
	db := budgetDB(t)
	ctx := context.Background()
	if privateRuntime {
		if _, err := db.Exec(ctx, `REVOKE TEMP ON DATABASE devpilot_registration_test FROM PUBLIC`); err != nil {
			t.Fatal(err)
		}
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
	catalog := p.exchange("catalog", nil)
	f, m := monitorFixture(t, db)
	url := "https://a.fixture.example.com/fixture/private-path-SENTINEL"
	interval, timeout := int64(60), int64(10)
	var e error
	m, e = (monitor.Store{Pool: db}).Update(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.Input{URL: &url, IntervalSeconds: &interval, TimeoutSeconds: &timeout}, monitor.ETag(m))
	if e != nil {
		t.Fatal(e)
	}
	server, client, _ := checkpointHTTPServer(t, db)
	defer server.Close()
	a, h, _ := checkpointProjectionGet(t, client, server.URL, status75Base(m), f.ownerToken)
	body := fmt.Sprintf(`{"configuration_version":%q,"consent_profile":"periodic-http-get-v1","authorization_seconds":900}`, a.ConfigurationVersion)
	code, _, raw := checkpointHTTP(t, client, server.URL, "PUT", status75Base(m)+"/execution-authorization", body, f.ownerToken, h.Get("ETag"), nil)
	if code != 200 {
		t.Fatal("real Owner HTTP authorization", code, string(raw))
	}
	baseline := status75ReadPair(t, client, server.URL, m, f.ownerToken)
	if baseline.s.AuthorizationState != "valid" || !baseline.s.EffectiveAuthorization {
		t.Fatal("fixture Owner authorization ineffective")
	}
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
	for cycle := 0; cycle < 3; cycle++ {
		winner := -1
		var tick periodicTick
		for winner < 0 {
			if time.Since(began) > 240*time.Second {
				t.Fatal("three real cycles exceeded bounded240s")
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
		if lease.RunID == previousRun || lease.FencingToken != fmt.Sprint(cycle+1) || lease.ConfigurationVersion != baseline.a.ConfigurationVersion || lease.ActivationGeneration != baseline.a.ActivationGeneration {
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
				if privateRuntime && cycle == 0 {
					var before, after string
					const state = `SELECT jsonb_build_object('run',to_jsonb(r),'reservation',(SELECT to_jsonb(q) FROM scheduler_reservations q WHERE q.run_id=r.run_id),'budgets',(SELECT jsonb_agg(to_jsonb(b) ORDER BY kind,budget_key) FROM scheduler_budget_state b))::text FROM monitor_runs r WHERE r.run_id=$1`
					if err := db.QueryRow(ctx, state, line.RunID).Scan(&before); err != nil {
						t.Fatal(err)
					}
					if _, err := supervisorfixture.VerifyCommittedStopBundle([]byte(`{"physical_release":true}`), []byte(catalog.PublicKey), []byte(`{}`), line); err == nil {
						t.Fatal("untrusted stop assertion accepted")
					}
					if _, err := (supervisorfixture.Settlement{DB: s, Incarnation: si}).Accept(ctx, supervisorfixture.VerifiedStop{}); err == nil {
						t.Fatal("unverified stop released resources")
					}
					_, err := []*pgxpool.Pool{w1, w2}[winner].Exec(ctx, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp() WHERE run_id=$1`, line.RunID)
					var denied *pgconn.PgError
					if !errors.As(err, &denied) || denied.Code != "42501" {
						t.Fatal("Worker acquired physical release authority")
					}
					p.exchange("worker-private-runtime", boots[winner])
					if err := db.QueryRow(ctx, state, line.RunID).Scan(&after); err != nil || before != after {
						t.Fatal("closed Worker exit/restart changed Run or reservation")
					}
				}
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
		pair := status75ReadPair(t, client, server.URL, m, f.ownerToken)
		if pair.tag != baseline.tag || string(pair.activation) != string(baseline.activation) || pair.s.InFlight || pair.s.LatestAttempt == nil || pair.s.CurrentObservation == nil || pair.s.NextDue == nil {
			t.Fatal("periodic result/authorization projection invalid")
		}
		var evidence []byte
		var due, finished, started time.Time
		var nextID string
		if e = db.QueryRow(ctx, `SELECT s.next_due_at,r.finished_at,r.started_at,s.next_run_id::text,jsonb_build_object('event','cycle_completed','at',clock_timestamp(),'cycle',$2::int,'worker',r.lease_owner,'run',to_jsonb(r),'schedule',to_jsonb(s),'reservation',to_jsonb(q)) FROM monitor_runs r JOIN monitor_schedules s USING(workspace_id,project_id,monitor_id) JOIN scheduler_reservations q USING(workspace_id,project_id,monitor_id,run_id) WHERE r.run_id=$1 AND r.attempt_state='completed' AND r.http_status=200 AND r.success AND r.failure_code IS NULL AND r.result_commit_id IS NOT NULL AND r.duration_ms BETWEEN 0 AND 10000 AND r.physical_release_confirmed_at=q.released_at AND q.reservation_state='released' AND q.release_basis='supervisor' AND s.latest_result_run_id=r.run_id AND s.last_attempt_run_id=r.run_id`, line.RunID, cycle+1).Scan(&due, &finished, &started, &nextID, &evidence); e != nil {
			t.Fatal("minimal persisted Run/Result/due contract", e)
		}
		if due.Before(finished.Add(60*time.Second)) || due.Before(started.Add(60*time.Second)) || nextID == line.RunID || !due.Equal(*pair.s.NextDue) {
			t.Fatal("next due did not preserve real60s interval")
		}
		p.exchange("cycle-completed", json.RawMessage(evidence))
		previousDue = &due
		previousRun = line.RunID
	}
	if attempts[0] < 3 || attempts[1] < 3 {
		t.Fatal("both Workers did not participate in all cycles")
	}
	p.exchange("workers-stop", nil)
	if queryInt(t, db, `SELECT count(*) FROM monitor_runs`) != 3 || queryInt(t, db, `SELECT in_use FROM scheduler_budget_state WHERE kind='global'`) != 0 {
		t.Fatal("unexpected extra Run or retained physical slot")
	}
	t.Log("three actual60s cycles; two isolated Worker containers; real Owner HTTP; trusted physical release; no production egress or disk-capacity claim")
}

// Targeted forward/down/up verification, including the exact pre-existing ACL.
// The original Migration10 lifecycle test also runs, with its schema10 assertions.
func TestPeriodicMigrationRoundTripPG(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()
	dir := root() + "/migrations"
	goose(t, dir, "down-to 12", true) // Pin this existing lifecycle assertion to its own migration.
	signature := "scheduler_private.commit_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,uuid,integer,integer,boolean,text)"
	snapshot := func() (string, string) {
		t.Helper()
		var definition, identity string
		if e := db.QueryRow(ctx, `SELECT pg_get_functiondef(oid),jsonb_build_object('owner',pg_get_userbyid(proowner),'acl',proacl,'config',proconfig)::text FROM pg_proc WHERE oid=$1::regprocedure`, signature).Scan(&definition, &identity); e != nil {
			t.Fatal(e)
		}
		return definition, identity
	}
	definition, identity := snapshot()
	if !strings.Contains(definition, "next_due_at=GREATEST") {
		t.Fatal("forward completion scheduling missing")
	}
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='fixture',emergency_stop=false WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", false) // A live gate must reject rollback, even after release.
	if _, e := db.Exec(ctx, `UPDATE scheduler_control SET gate='closed',emergency_stop=true WHERE control_id='deployment'`); e != nil {
		t.Fatal(e)
	}
	goose(t, dir, "down", true)
	old, oldIdentity := snapshot()
	if strings.Contains(old, "next_due_at=GREATEST") || identity != oldIdentity || queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 10 {
		t.Fatal("Down did not restore accepted function/ACL")
	}
	goose(t, dir, "up-to 12", true)
	goose(t, dir, "up-to 12", true)
	current, currentIdentity := snapshot()
	if current != definition || currentIdentity != identity || queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 12 || queryInt(t, db, `SELECT count(*) FROM goose_db_version WHERE version_id=11 AND is_applied`) != 0 {
		t.Fatal("forward/up-up changed definition or privilege boundary")
	}
	for _, role := range []string{"devpilot_scheduler_worker", "devpilot_scheduler_supervisor", "devpilot_scheduler_controller", "devpilot_scheduler_api"} {
		var allowed bool
		if e := db.QueryRow(ctx, `SELECT has_function_privilege($1,$2,'EXECUTE')`, role, signature).Scan(&allowed); e != nil || allowed != (role == "devpilot_scheduler_worker") {
			t.Fatal("function ACL expanded", role, e)
		}
	}
	t.Log("Migration12 down/up/up: owner, ACL, search_path,250ms lock setting unchanged;11 absent; live-gate rollback rejected")
}
