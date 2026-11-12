//go:build schedulerfixture && linux

package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

func TestWorkerBurnsStartBeforeUnknownCommit(t *testing.T) {
	v := startTestWire()
	v.PermitDeadline = v.StartedAt.Add(35 * time.Second)
	raw, _ := json.Marshal(v)
	tx := &schedulerFakeTx{raw: raw, commitErr: io.ErrUnexpectedEOF}
	w := NewFixtureWorker(Store{DB: schedulerFakeDB{tx}})
	h := Hint{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID}
	w.hint = h
	w.lease = &v.Lease
	if _, err := w.Start(context.Background(), h, v.Lease); !errors.Is(err, ErrCommitUnknown) {
		t.Fatal(err)
	}
	if _, err := w.Start(context.Background(), h, v.Lease); !errors.Is(err, ErrInputConsumed) {
		t.Fatal("Start was retried", err)
	}
	if tx.commits != 1 {
		t.Fatal("Start reached DB twice")
	}
}
func TestWorkerDoesNotRetryUnknownClaim(t *testing.T) {
	v := startTestWire()
	raw, _ := json.Marshal(v.Lease)
	tx := &schedulerFakeTx{raw: raw, commitErr: io.ErrUnexpectedEOF}
	w := NewFixtureWorker(Store{DB: schedulerFakeDB{tx}})
	h := Hint{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID, ConfigurationVersion: v.ConfigurationVersion, ActivationGeneration: v.ActivationGeneration, SchedulerEpoch: v.SchedulerEpoch}
	if _, err := w.Claim(context.Background(), h); !errors.Is(err, ErrCommitUnknown) {
		t.Fatal(err)
	}
	if _, err := w.Claim(context.Background(), h); !errors.Is(err, ErrCandidate) {
		t.Fatal("Claim was retried", err)
	}
	if tx.commits != 1 {
		t.Fatal("Claim reached DB twice")
	}
}
