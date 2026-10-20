//go:build schedulerfixture && linux

package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func startTestWire() startWire {
	return startWire{Lease: Lease{WorkspaceID: "01900000-0000-7000-8000-000000000001", ProjectID: "01900000-0000-7000-8000-000000000002", MonitorID: "01900000-0000-7000-8000-000000000003", RunID: "01900000-0000-7000-8000-000000000004", ConfigurationVersion: "9223372036854775807", ActivationGeneration: "9223372036854775807", SchedulerEpoch: "9223372036854775807", FencingToken: "9223372036854775807"}, StartedAt: time.Now().UTC(), PermitDeadline: time.Now().Add(35 * time.Second).UTC(), URL: "https://example.com/private-sentinel", TimeoutSeconds: 30, ExpectedStatus: 200, IntervalSeconds: 60, NamespaceIncarnation: "01900000-0000-7000-8000-000000000005", SupervisorIncarnation: "01900000-0000-7000-8000-000000000006"}
}
func TestExecutionInputIsBoundedSingleUseAndRedacted(t *testing.T) {
	v := startTestWire()
	v.URL = "https://example.com/" + strings.Repeat(`"\\&<>`, 400)
	v.URL = v.URL[:2048]
	v.PermitDeadline = v.StartedAt.Add(35 * time.Second)
	s := StartSnapshot{input: &startInput{wire: v}}
	for _, f := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if strings.Contains(fmt.Sprintf(f, s), "example.com") {
			t.Fatal("snapshot leaked")
		}
	}
	if b, err := json.Marshal(s); err != nil || string(b) != "{}" {
		t.Fatal("generic snapshot serialization leaked", err)
	}
	var output bytes.Buffer
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func(copy StartSnapshot) {
			defer wg.Done()
			err := copy.WriteInput(&output)
			if err == nil {
				accepted.Add(1)
			} else if !errors.Is(err, ErrInputConsumed) {
				t.Errorf("input error %v", err)
			}
		}(s)
	}
	wg.Wait()
	if accepted.Load() != 1 || output.Len() > 4096 {
		t.Fatal("input replay or unbounded envelope", accepted.Load(), output.Len())
	}
	var e executionEnvelope
	if json.Unmarshal(output.Bytes(), &e) != nil || string(e.Target) != v.URL || e.RunID != v.RunID || e.FencingToken != v.FencingToken {
		t.Fatal("envelope lost frozen lineage")
	}
	var empty StartSnapshot
	if !errors.Is(empty.WriteInput(io.Discard), ErrInputConsumed) {
		t.Fatal("empty snapshot granted input")
	}
}

type failedInput struct{}

func (failedInput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestFailedExecutionInputCannotBeRetried(t *testing.T) {
	s := StartSnapshot{input: &startInput{wire: startTestWire()}}
	if !errors.Is(s.WriteInput(failedInput{}), ErrUnavailable) || !errors.Is(s.WriteInput(io.Discard), ErrInputConsumed) {
		t.Fatal("failed write restored capability")
	}
}

type schedulerFakeRow struct{ raw []byte }

func (r schedulerFakeRow) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("unexpected destinations")
	}
	p, ok := dest[0].(*[]byte)
	if !ok {
		return errors.New("unexpected destination")
	}
	*p = append([]byte(nil), r.raw...)
	return nil
}

type schedulerFakeTx struct {
	pgx.Tx
	raw                []byte
	commitErr          error
	commits, rollbacks int
}

func (t *schedulerFakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return schedulerFakeRow{t.raw}
}
func (t *schedulerFakeTx) Commit(context.Context) error   { t.commits++; return t.commitErr }
func (t *schedulerFakeTx) Rollback(context.Context) error { t.rollbacks++; return nil }

type schedulerFakeDB struct{ tx *schedulerFakeTx }

func (d schedulerFakeDB) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	if o.IsoLevel != pgx.ReadCommitted {
		return nil, errors.New("wrong isolation")
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
		return nil, errors.New("unbounded transaction")
	}
	return d.tx, nil
}
func (d schedulerFakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	return schedulerFakeRow{d.tx.raw}
}
func TestUnknownStartCommitReturnsNoExecutionCapability(t *testing.T) {
	v := startTestWire()
	v.PermitDeadline = v.StartedAt.Add(35 * time.Second)
	raw, _ := json.Marshal(v)
	h := Hint{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID}
	tx := &schedulerFakeTx{raw: raw, commitErr: io.ErrUnexpectedEOF}
	s := Store{DB: schedulerFakeDB{tx}}
	snapshot, err := s.Start(context.Background(), h, v.Lease)
	if !errors.Is(err, ErrCommitUnknown) || tx.commits != 1 || tx.rollbacks != 1 {
		t.Fatal("unknown commit was retried or accepted", err)
	}
	var b bytes.Buffer
	if !errors.Is(snapshot.WriteInput(&b), ErrInputConsumed) || b.Len() != 0 {
		t.Fatal("unconfirmed Start exposed envelope")
	}
}
func TestUnknownClaimRetainsOriginalCycleIdentity(t *testing.T) {
	v := startTestWire()
	raw, _ := json.Marshal(v.Lease)
	tx := &schedulerFakeTx{raw: raw, commitErr: io.ErrUnexpectedEOF}
	s := Store{DB: schedulerFakeDB{tx}}
	h := Hint{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID}
	got, err := s.Claim(context.Background(), h)
	var unknown ClaimUnknown
	if !errors.As(err, &unknown) || unknown.Hint != h || got.RunID != "" || tx.commits != 1 {
		t.Fatal("unknown claim lost identity or returned lease", err)
	}
}
