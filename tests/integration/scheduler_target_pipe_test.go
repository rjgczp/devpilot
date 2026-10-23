//go:build integration && schedulerfixture && linux && budgetfixture

package integration

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/monitoring/targetpermit"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// This witness validates only call ordering/protocol binding. It never installs
// rules and is NOT a Supervisor physical or network acceptance substitute.
type targetProtocolWitness struct {
	t        *testing.T
	db, w, s *pgxpool.Pool
	calls    atomic.Int32
	outcome  string
}

func (w *targetProtocolWitness) InstallTarget(ctx context.Context, r targetpermit.Request, p scheduler.TargetPermit) (targetpermit.Confirmation, error) {
	w.calls.Add(1)
	assertNoDBDuringInput(w.t, w.db, w.w, w.s)
	if queryInt(w.t, w.db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND target_permit_id=$2 AND target_permit_committed_at IS NOT NULL`, r.RunID, p.PermitID) != 1 {
		w.t.Error("Supervisor called before real PG commit")
	}
	a := targetpermit.Confirmation{Request: r, PermitID: p.PermitID, Deadline: r.Deadline}
	switch w.outcome {
	case "disconnect":
		return targetpermit.Confirmation{}, errors.New("protocol witness disconnected")
	case "wrong-ack":
		a.Request.FencingToken = "999"
	case "late-ack":
		<-ctx.Done()
	}
	return a, nil
}
func targetPipePair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}
func targetProtocolStart(t *testing.T, db, w, s *pgxpool.Pool, wi, si string, h schedulerClaimHint) (scheduler.StartSnapshot, targetpermit.Request) {
	t.Helper()
	ctx := context.Background()
	store := scheduler.Store{DB: w, Incarnation: wi}
	lease, e := store.Claim(ctx, checkpointTwoHint(h))
	if e != nil {
		t.Fatal(e)
	}
	budgetPrepare(t, db, s, si, wi, h)
	snapshot, e := store.Start(ctx, checkpointTwoHint(h), lease)
	if e != nil {
		t.Fatal(e)
	}
	pipes, e := snapshot.IssueInput(ctx, scheduler.InputController{DB: s, Incarnation: si})
	if e != nil {
		t.Fatal(e)
	}
	defer pipes.Close()
	receipt, e := handoff.ReadReceipt(pipes.Receipt)
	if e != nil {
		t.Fatal(e)
	}
	return snapshot, targetpermit.Request{Lineage: receipt.Lineage, Host: "a.fixture.example.com", Scheme: "https", IP: netip.MustParseAddr("192.0.2.10"), Port: 443, Deadline: time.Now().UTC().Add(time.Second)}
}
func targetProtocolDirectory(t *testing.T, raw string) scheduler.TargetDirectory {
	t.Helper()
	d, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte("12345678901234567890123456789012")}, []scheduler.FixtureTarget{{URL: raw, Addresses: []netip.Addr{netip.MustParseAddr("192.0.2.10")}}})
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestBudgetTargetPipeCommitBoundary(t *testing.T) {
	db := budgetDB(t)
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	c, ci := budgetController(t, db)
	outcomes := []string{"success", "lost", "rollback", "no-supervisor", "disconnect", "wrong-ack", "late-ack"}
	var hosts, ips []string
	for i, o := range outcomes {
		hosts = append(hosts, o+".fixture.example.com")
		ips = append(ips, fmt.Sprintf("192.0.2.%d", 10+i))
	}
	hs, urls := budgetTargetFixtures(t, db, hosts)
	es := budgetCatalog(t, hs, urls, ips, map[int64][]byte{1: []byte("12345678901234567890123456789012")})
	budgetSQLState(t, budgetRotate(t, c, ci, 1, es), "00000")
	for i, outcome := range outcomes {
		t.Run(outcome, func(t *testing.T) {
			h := hs[i]
			snapshot, r := targetProtocolStart(t, db, w, s, wi, si, h)
			r.Host = hosts[i]
			r.IP = netip.MustParseAddr(ips[i])
			if outcome == "late-ack" {
				r.Deadline = time.Now().UTC().Add(80 * time.Millisecond)
			}
			dir, e := scheduler.NewTargetDirectory(map[int64][]byte{1: []byte("12345678901234567890123456789012")}, []scheduler.FixtureTarget{{URL: urls[i], Addresses: []netip.Addr{r.IP}}})
			if e != nil {
				t.Fatal(e)
			}
			store := scheduler.Store{DB: w, Incarnation: wi}
			if outcome == "lost" || outcome == "rollback" {
				store.DB = workspaceObservedDB{Pool: w, wrap: func(tx pgx.Tx) pgx.Tx { return workspaceObservedTx{Tx: tx, commitOutcome: outcome} }}
			}
			witness := &targetProtocolWitness{t: t, db: db, w: w, s: s, outcome: outcome}
			var supervisor scheduler.TargetSupervisor = witness
			if outcome == "no-supervisor" {
				supervisor = nil
			}
			controller := snapshot.TargetController(store, checkpointTwoHint(h), dir, supervisor)
			rr, rw := targetPipePair(t)
			ar, aw := targetPipePair(t)
			client := targetpermit.NewClient(rw, ar)
			done := make(chan error, 1)
			go func() { done <- (&targetpermit.Server{}).Serve(context.Background(), rr, aw, controller.Authorize) }()
			err := client.Acquire(context.Background(), r)
			serverErr := <-done
			if outcome == "success" {
				if err != nil || serverErr != nil {
					t.Fatal("confirmed offline protocol", err, serverErr)
				}
			} else if !errors.Is(err, targetpermit.ErrDenied) {
				t.Fatal("uncertainty exposed allow", outcome, err)
			}
			if outcome == "lost" || outcome == "rollback" || outcome == "no-supervisor" {
				if witness.calls.Load() != 0 {
					t.Fatal("unconfirmed DB called installer")
				}
			} else if witness.calls.Load() != 1 {
				t.Fatal("installer invocation count")
			}
			want := 1
			if outcome == "rollback" || outcome == "no-supervisor" {
				want = 0
			}
			if queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE run_id=$1 AND target_permit_id IS NOT NULL`, h.r) != want {
				t.Fatal("real target commit outcome")
			}
			again := snapshot.TargetController(scheduler.Store{DB: w, Incarnation: wi}, checkpointTwoHint(h), dir, witness)
			if _, e := again.Authorize(context.Background(), r); !errors.Is(e, targetpermit.ErrDenied) {
				t.Fatal("reconstructed controller restored attempt")
			}
			if client.Acquire(context.Background(), r) == nil {
				t.Fatal("reconnected pipe restored allow")
			}
			assertNoDBDuringInput(t, db, w, s)
			t.Logf("actual PG commit=%s held_target_reservations=%d installer_calls=%d; no target network; protocol witness is not physical Supervisor proof", outcome, want, witness.calls.Load())
		})
	}
}
func TestBudgetTargetPipeDeadlineBarrier(t *testing.T) {
	db := budgetDB(t)
	hs, urls := budgetTargetFixtures(t, db, []string{"a.fixture.example.com"})
	h := hs[0]
	d := targetProtocolDirectory(t, urls[0])
	entry, e := d.Bind(checkpointTwoHint(h), urls[0], netip.MustParseAddr("192.0.2.10"))
	if e != nil {
		t.Fatal(e)
	}
	c, ci := budgetController(t, db)
	budgetSQLState(t, budgetRotate(t, c, ci, 1, []scheduler.CatalogEntry{entry}), "00000")
	w, wi := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	s, si := schedulerSupervisorLogin(t, db)
	snapshot, r := targetProtocolStart(t, db, w, s, wi, si, h)
	r.Deadline = time.Now().UTC().Add(180 * time.Millisecond)
	barrier, e := db.Begin(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer barrier.Rollback(context.Background())
	if _, e = barrier.Exec(context.Background(), `SELECT budget_key FROM scheduler_budget_state WHERE kind='origin' AND budget_key=$1 FOR UPDATE`, entry.Keys[0].OriginKey); e != nil {
		t.Fatal(e)
	}
	pid := make(chan int, 1)
	store := scheduler.Store{DB: workspaceObservedDB{Pool: w, pid: pid}, Incarnation: wi}
	witness := &targetProtocolWitness{t: t, db: db, w: w, s: s}
	controller := snapshot.TargetController(store, checkpointTwoHint(h), d, witness)
	done := make(chan error, 1)
	go func() { _, err := controller.Authorize(context.Background(), r); done <- err }()
	p := <-pid
	waitActivationBlock(t, db, p)
	schedulerLockReceipt(t, db, "target-pipe-budget-wait-crosses-request-deadline", p)
	if err := <-done; !errors.Is(err, targetpermit.ErrDenied) {
		t.Fatal("deadline allowed target", err)
	}
	if e = barrier.Rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	if witness.calls.Load() != 0 || queryInt(t, db, `SELECT count(*) FROM scheduler_reservations WHERE target_permit_id IS NOT NULL`) != 0 {
		t.Fatal("deadline minted target permit")
	}
	t.Log("real independent connection blocked on origin lock; context deadline canceled PG statement, zero installer calls, no target dial; raw SQLSTATE in PG log")
}
