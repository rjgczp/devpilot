//go:build privateworker && privateprobe && linux

package supervisorruntime

import (
	"context"
	"encoding/json"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/workerruntime"
	"testing"
)

type backendFunc func(context.Context, workerruntime.Call) (BackendReply, error)

func (f backendFunc) Call(c context.Context, r workerruntime.Call) (BackendReply, error) {
	return f(c, r)
}

type controlFunc func(context.Context, workerruntime.Call) (workerruntime.Reply, error)

func (f controlFunc) Call(c context.Context, r workerruntime.Call) (workerruntime.Reply, error) {
	return f(c, r)
}
func lineage() handoff.Lineage {
	return handoff.Lineage{WorkspaceID: "00000000-0000-4000-8000-000000000001", ProjectID: "00000000-0000-4000-8000-000000000002", MonitorID: "00000000-0000-4000-8000-000000000003", RunID: "00000000-0000-4000-8000-000000000004", NamespaceIncarnation: "00000000-0000-4000-8000-000000000005", SupervisorIncarnation: "00000000-0000-4000-8000-000000000006", ConfigurationVersion: "1", ActivationGeneration: "2", SchedulerEpoch: "1", FencingToken: "3"}
}
func TestForeignLineAndForgedPhysicalEvidenceNeverReachSQL(t *testing.T) {
	line := lineage()
	s := Supervisor{active: &line}
	other := line
	other.FencingToken = "4"
	if _, e := s.Handle(context.Background(), workerruntime.Call{Op: "settle", Line: &other}); e == nil {
		t.Fatal("foreign fence accepted")
	}
	s.Controller = controlFunc(func(_ context.Context, c workerruntime.Call) (workerruntime.Reply, error) {
		if c.Op != "quarantine" {
			t.Fatal(c.Op)
		}
		return workerruntime.Reply{Event: "quarantine_ok", Line: line}, nil
	})
	s.Kernel = backendFunc(func(_ context.Context, c workerruntime.Call) (BackendReply, error) {
		return BackendReply{Event: "stopped", Line: line, Receipt: []byte(`{"released":true}`), Bundle: []byte(`{}`)}, nil
	})
	if _, e := s.Handle(context.Background(), workerruntime.Call{Op: "settle", Line: &line}); e == nil {
		t.Fatal("invented release accepted")
	}
}
func TestUnknownOrSQLIPCRejected(t *testing.T) {
	var c workerruntime.Call
	for _, raw := range []string{`{"op":"sql","query":"DELETE FROM monitor_runs"}`, `{"op":"hello","op":"prepare"}`, `{"op":`} {
		if decode([]byte(raw), &c) == nil {
			t.Fatal("damaged/unknown control frame", raw)
		}
	}
	ctrl := Controller{}
	line := lineage()
	if _, e := ctrl.RecoveryHandle(context.Background(), workerruntime.Call{Op: "track", Line: &line}); e == nil {
		t.Fatal("parent channel accepted non-recovery operation")
	}
	if _, e := ctrl.Handle(context.Background(), workerruntime.Call{Op: "quarantine", Line: &line}); e == nil {
		t.Fatal("untracked quarantine")
	}
	if _, e := ctrl.Handle(context.Background(), workerruntime.Call{Op: "track", Line: &line, Frame: []byte("GET")}); e == nil {
		t.Fatal("execution payload reached Controller")
	}
}
func TestRestartWithoutProofDoesNotIssueAnyExecutionCall(t *testing.T) {
	calls := 0
	s := Supervisor{Kernel: backendFunc(func(_ context.Context, c workerruntime.Call) (BackendReply, error) {
		calls++
		if c.Op != "pending" {
			t.Fatal("restart attempted execution", c.Op)
		}
		return BackendReply{Event: "none"}, nil
	})}
	r, e := s.Handle(context.Background(), workerruntime.Call{Op: "hello"})
	if e != nil || r.Event != "supervisor_ready" || calls != 1 {
		t.Fatal(r, e, calls)
	}
}
func TestControllerTrackingBoundAndMalformedFrames(t *testing.T) {
	ctrl := Controller{}
	ctx := context.Background()
	line := lineage()
	for i := 0; i < 2; i++ {
		x := line
		x.RunID = line.RunID[:35] + string(rune('1'+i))
		if _, e := ctrl.Handle(ctx, workerruntime.Call{Op: "track", Line: &x}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := ctrl.Handle(ctx, workerruntime.Call{Op: "track", Line: &line}); e == nil {
		t.Fatal("unbounded tracking")
	}
	raw, _ := json.Marshal(workerruntime.Call{Op: "recover", Line: &line})
	var x workerruntime.Call
	if decode(raw, &x) != nil {
		t.Fatal("typed frame")
	}
}

func TestControlFinalizationRequiresAcceptedExactPhysicalLine(t *testing.T) {
	line := lineage()
	calls := 0
	s := Supervisor{Controller: controlFunc(func(_ context.Context, c workerruntime.Call) (workerruntime.Reply, error) {
		calls++
		if c.Op != "recover" || c.Line == nil || *c.Line != line {
			t.Fatal("not exact existing recovery")
		}
		return workerruntime.Reply{Event: "recover_ok", Line: line}, nil
	}), Kernel: backendFunc(func(_ context.Context, c workerruntime.Call) (BackendReply, error) {
		calls++
		if c.Op != "recovered" {
			t.Fatal("finalization attempted execution or release", c.Op)
		}
		return BackendReply{Event: "recovered", Line: line}, nil
	})}
	if _, e := s.Handle(context.Background(), workerruntime.Call{Op: "finalize_control", Line: &line}); e == nil || calls != 0 {
		t.Fatal("unverified physical line finalized")
	}
	s.settled = &line
	foreign := line
	foreign.FencingToken = "99"
	if _, e := s.Handle(context.Background(), workerruntime.Call{Op: "finalize_control", Line: &foreign}); e == nil || calls != 0 {
		t.Fatal("foreign fence finalized")
	}
	r, e := s.Handle(context.Background(), workerruntime.Call{Op: "finalize_control", Line: &line})
	if e != nil || r.Event != "control_finalized" || calls != 2 {
		t.Fatal(r, e, calls)
	}
	if _, e = s.Handle(context.Background(), workerruntime.Call{Op: "finalize_control", Line: &line}); e == nil || calls != 2 {
		t.Fatal("finalization replayed")
	}
}
