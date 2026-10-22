//go:build integration && schedulerfixture && linux && budgetfixture && networkfixture

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
	"github.com/rjgczp/devpilot/internal/supervisorfixture"
)

// The external, trusted Docker allocator owns this stdio channel. It actually
// runs NET_ADMIN supervisors in separate namespaces. The DB driver has no
// NET_ADMIN/Docker socket and is not the unprivileged network test child.
type networkPolicyBroker struct {
	t        *testing.T
	db, w, s *pgxpool.Pool
	in       *bufio.Scanner
}

type networkPolicyReply struct {
	Ready            map[string]supervisorfixture.Ready `json:"ready"`
	Confirmation     targetpermit.Confirmation          `json:"confirmation"`
	IP               string                             `json:"ip"`
	TeardownComplete bool                               `json:"fixture_teardown_complete"`
}

func (b *networkPolicyBroker) exchange(op string, payload any) networkPolicyReply {
	b.t.Helper()
	raw, e := json.Marshal(struct {
		Op      string `json:"op"`
		Payload any    `json:"payload"`
	}{op, payload})
	if e != nil {
		b.t.Fatal(e)
	}
	// This is a private fixture control stream, not an ordinary application log.
	fmt.Fprintln(os.Stdout, "NETWORK64 "+string(raw))
	if !b.in.Scan() {
		b.t.Fatal("independent Linux fixture management channel unavailable")
	}
	var reply networkPolicyReply
	if supervisorfixture.Decode(b.in.Bytes(), &reply) != nil {
		b.t.Fatal("invalid fixture management confirmation")
	}
	return reply
}

func (b *networkPolicyBroker) InstallTarget(ctx context.Context, r targetpermit.Request, p scheduler.TargetPermit) (targetpermit.Confirmation, error) {
	assertNoDBDuringInput(b.t, b.db, b.w, b.s)
	if queryInt(b.t, b.db, `SELECT count(*) FROM scheduler_reservations WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4 AND target_permit_id=$5 AND target_permit_committed_at IS NOT NULL`, r.WorkspaceID, r.ProjectID, r.MonitorID, r.RunID, p.PermitID) != 1 {
		b.t.Fatal("network install before scoped target Commit")
	}
	reply := b.exchange("install", supervisorfixture.Install{Request: r, PermitID: p.PermitID, CommittedAt: p.CommittedAt, PermitDeadline: p.PermitDeadline})
	if !reply.Confirmation.Matches(r) || ctx.Err() != nil {
		return targetpermit.Confirmation{}, targetpermit.ErrDenied
	}
	return reply.Confirmation, nil
}

func TestBudgetNetworkPolicyPG(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	h := hs[0]
	store := scheduler.Store{DB: w, Incarnation: wi}
	ctx := context.Background()
	broker := &networkPolicyBroker{t: t, db: db, w: w, s: s, in: bufio.NewScanner(os.Stdin)}
	broker.in.Buffer(make([]byte, 4096), 4097)
	selection := broker.exchange("catalog", nil)
	ip, e := netip.ParseAddr(selection.IP)
	if e != nil {
		t.Fatal(e)
	}
	directory, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte("12345678901234567890123456789012")}, []scheduler.FixtureTarget{{URL: urls[0], Addresses: []netip.Addr{ip}}})
	if e != nil {
		t.Fatal(e)
	}
	entry, e := directory.Bind(checkpointTwoHint(h), urls[0], ip)
	if e != nil {
		t.Fatal(e)
	}
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	lease, e := store.Claim(ctx, checkpointTwoHint(h))
	if e != nil {
		t.Fatal(e)
	}
	var ns string
	if e = db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&ns); e != nil {
		t.Fatal(e)
	}
	l := handoff.Lineage{WorkspaceID: h.w, ProjectID: h.p, MonitorID: h.m, RunID: h.r, ConfigurationVersion: lease.ConfigurationVersion, ActivationGeneration: lease.ActivationGeneration, SchedulerEpoch: lease.SchedulerEpoch, FencingToken: lease.FencingToken, NamespaceIncarnation: ns, SupervisorIncarnation: si}
	ready := broker.exchange("prepare", l)
	encoded, _ := json.Marshal(l)
	lineageHash := sha256.Sum256(encoded)
	if len(ready.Ready) != 2 {
		t.Fatal("both independent network policies required")
	}
	for role, p := range ready.Ready {
		if p.Role != role || p.LineageHash != hex.EncodeToString(lineageHash[:]) || p.Namespace == "" || len(p.RulesHash) != 64 || !p.HardDeadline.After(time.Now()) {
			t.Fatal("mismatched namespace proof")
		}
	}
	if ready.Ready["run"].Namespace == ready.Ready["gateway"].Namespace {
		t.Fatal("run and independent guard share namespace")
	}
	proofBytes, _ := json.Marshal(ready.Ready)
	proof := sha256.Sum256(proofBytes)
	// Existing Supervisor-only capability, after actual rules verification.
	_, e = s.Exec(ctx, `SELECT scheduler_private.prepare_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,35000,$8::bytea)`, si, h.w, h.p, h.m, h.r, wi, ns, proof[:])
	budgetSQLState(t, e, "00000")
	snapshot, e := store.Start(ctx, checkpointTwoHint(h), lease)
	if e != nil {
		t.Fatal(e)
	}
	pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
	if e != nil {
		t.Fatal(e)
	}
	receipt, e := handoff.ReadReceipt(pipes.Receipt)
	pipes.Close()
	if e != nil {
		t.Fatal(e)
	}
	if receipt.Lineage != l {
		t.Fatal("Start input lineage differs from prepared namespace")
	}
	r := targetpermit.Request{Lineage: l, Host: "a.fixture.example.com", Scheme: "https", IP: ip, Port: 443, Deadline: time.Now().UTC().Add(5 * time.Second)}
	controller := snapshot.TargetController(store, checkpointTwoHint(h), directory, broker)
	a, e := controller.Authorize(ctx, r)
	if e != nil || !a.Matches(r) {
		t.Fatal("independent Linux target install failed", e)
	}
	if _, e = controller.Authorize(ctx, r); e == nil {
		t.Fatal("second per-Run target capability")
	}
	if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE workspace_id=$1 AND project_id=$2 AND monitor_id=$3 AND run_id=$4 AND reservation_state='reserved' AND released_at IS NULL AND termination_receipt IS NULL`, h.w, h.p, h.m, h.r) != 1 {
		t.Fatal("target confirmation prematurely returned physical capacity")
	}
	// Host now witnesses the exact allowed fixture GET and destroys only the
	// disposable namespaces. This is teardown, not a task6.5 termination receipt.
	if !broker.exchange("witness", a).TeardownComplete {
		t.Fatal("fixture namespaces not torn down before DB cleanup")
	}
	t.Log("SQLSTATE=00000 confirmed Start/target Commit; both real namespace policies; no business DB connection across install; physical ledger remains held, no release receipt")
}
