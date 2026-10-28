//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"sync/atomic"
	"testing"
	"time"
)

type boundedDB struct {
	hints  []Hint
	begins atomic.Int32
	busy   bool
}

func (d *boundedDB) QueryRow(context.Context, string, ...any) pgx.Row {
	raw, _ := json.Marshal(d.hints)
	return schedulerFakeRow{raw}
}
func (d *boundedDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	d.begins.Add(1)
	return &boundedTx{db: d}, nil
}

type boundedTx struct {
	pgx.Tx
	db *boundedDB
}

func (t *boundedTx) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if t.db.busy {
		return errorRow{&pgconn.PgError{Code: "P0001", Message: "candidate_busy"}}
	}
	h := Hint{WorkspaceID: args[1].(string), ProjectID: args[2].(string), MonitorID: args[3].(string), ConfigurationVersion: args[6].(string), ActivationGeneration: args[7].(string), SchedulerEpoch: args[8].(string), RunID: args[9].(string)}
	l := Lease{WorkspaceID: h.WorkspaceID, ProjectID: h.ProjectID, MonitorID: h.MonitorID, RunID: h.RunID, ConfigurationVersion: h.ConfigurationVersion, ActivationGeneration: h.ActivationGeneration, SchedulerEpoch: h.SchedulerEpoch, FencingToken: "1", ClaimedAt: time.Now(), LeaseUntil: time.Now().Add(45 * time.Second)}
	raw, _ := json.Marshal(l)
	return schedulerFakeRow{raw}
}
func (t *boundedTx) Commit(context.Context) error   { return nil }
func (t *boundedTx) Rollback(context.Context) error { return nil }

type errorRow struct{ err error }

func (r errorRow) Scan(...any) error { return r.err }
func TestTickBoundsAttemptsAndWorkspaceSuccess(t *testing.T) {
	for _, busy := range []bool{true, false} {
		t.Run(fmt.Sprint(busy), func(t *testing.T) {
			d := &boundedDB{busy: busy}
			for i := range 80 {
				d.hints = append(d.hints, Hint{WorkspaceID: fmt.Sprint(i / 8), ProjectID: "p", MonitorID: fmt.Sprint(i), RunID: fmt.Sprint(i), ConfigurationVersion: "1", ActivationGeneration: "1", SchedulerEpoch: "1"})
			}
			leases, _, err := (Store{DB: d}).Tick(context.Background(), "")
			if err != nil {
				t.Fatal(err)
			}
			want := int32(10)
			if busy {
				want = 32
			}
			if d.begins.Load() != want {
				t.Fatal("unbounded attempts", d.begins.Load())
			}
			if !busy && len(leases) != 10 {
				t.Fatal("duplicate W success")
			}
		})
	}
}
func TestFixedDatabaseErrorsAndNoSuccessOnMalformedLease(t *testing.T) {
	for _, code := range []string{"P0001", "55P03", "40P01", "57014"} {
		if !errors.Is(databaseError(&pgconn.PgError{Code: code}), ErrCandidate) {
			t.Fatal(code)
		}
	}
	if !errors.Is(databaseError(&pgconn.PgError{Code: "P0001", Message: "retention_capacity"}), ErrCapacity) {
		t.Fatal("capacity category")
	}
	tx := &schedulerFakeTx{raw: []byte(`{"run_id":"r"}`)}
	if _, err := (Store{DB: schedulerFakeDB{tx}}).Claim(context.Background(), Hint{RunID: "r"}); !errors.Is(err, ErrUnavailable) || tx.commits != 0 {
		t.Fatal("malformed/scopeless lease committed", err)
	}
}
