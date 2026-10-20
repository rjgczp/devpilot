//go:build schedulerfixture && linux

package scheduler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var ErrInputConsumed = errors.New("execution input already consumed")

// StartSnapshot is process-local and redacts all generic representations. It
// cannot be reconstructed from a run. Its only serialization is a bounded,
// once-only execution envelope written after a confirmed Start commit.
type StartSnapshot struct{ input *startInput }
type startInput struct {
	wire     startWire
	consumed atomic.Bool
}
type startWire struct {
	Lease
	StartedAt             time.Time `json:"started_at"`
	PermitDeadline        time.Time `json:"permit_deadline"`
	URL                   string    `json:"url"`
	TimeoutSeconds        int       `json:"timeout_seconds"`
	ExpectedStatus        int       `json:"expected_status"`
	IntervalSeconds       int       `json:"interval_seconds"`
	NamespaceIncarnation  string    `json:"namespace_incarnation"`
	SupervisorIncarnation string    `json:"supervisor_incarnation"`
}

func (StartSnapshot) Format(f fmt.State, _ rune) {
	_, _ = io.WriteString(f, "<scheduler start snapshot>")
}

// The URL uses JSON's base64 byte encoding in the private pipe protocol. This
// bounds worst-case JSON escaping of any valid 2048-byte configuration URL.
// Generic JSON marshalling of StartSnapshot itself still yields an empty object.
type executionEnvelope struct {
	WorkspaceID           string    `json:"workspace_id"`
	ProjectID             string    `json:"project_id"`
	MonitorID             string    `json:"monitor_id"`
	RunID                 string    `json:"run_id"`
	ConfigurationVersion  string    `json:"configuration_version"`
	ActivationGeneration  string    `json:"activation_generation"`
	SchedulerEpoch        string    `json:"scheduler_epoch"`
	FencingToken          string    `json:"fencing_token"`
	StartedAt             time.Time `json:"started_at"`
	PermitDeadline        time.Time `json:"permit_deadline"`
	NamespaceIncarnation  string    `json:"namespace_incarnation"`
	SupervisorIncarnation string    `json:"supervisor_incarnation"`
	Target                []byte    `json:"target"`
	TimeoutSeconds        int       `json:"timeout_seconds"`
	ExpectedStatus        int       `json:"expected_status"`
	IntervalSeconds       int       `json:"interval_seconds"`
}

func (s StartSnapshot) WriteInput(w io.Writer) error {
	if s.input == nil || !s.input.consumed.CompareAndSwap(false, true) {
		return ErrInputConsumed
	}
	v := s.input.wire
	e := executionEnvelope{WorkspaceID: v.WorkspaceID, ProjectID: v.ProjectID, MonitorID: v.MonitorID, RunID: v.RunID, ConfigurationVersion: v.ConfigurationVersion, ActivationGeneration: v.ActivationGeneration, SchedulerEpoch: v.SchedulerEpoch, FencingToken: v.FencingToken, StartedAt: v.StartedAt, PermitDeadline: v.PermitDeadline, NamespaceIncarnation: v.NamespaceIncarnation, SupervisorIncarnation: v.SupervisorIncarnation, Target: []byte(v.URL), TimeoutSeconds: v.TimeoutSeconds, ExpectedStatus: v.ExpectedStatus, IntervalSeconds: v.IntervalSeconds}
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(e) != nil || b.Len() > 4096 {
		return ErrUnavailable
	}
	if _, err := io.Copy(w, &b); err != nil {
		return ErrUnavailable
	}
	return nil
}
func (s Store) Start(ctx context.Context, h Hint, lease Lease) (StartSnapshot, error) {
	if lease.RunID != h.RunID || lease.WorkspaceID != h.WorkspaceID || lease.ProjectID != h.ProjectID || lease.MonitorID != h.MonitorID {
		return StartSnapshot{}, ErrCandidate
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return StartSnapshot{}, ErrUnavailable
	}
	defer rollback(tx)
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT scheduler_private.start_run($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::uuid,$8::bigint)`, s.Incarnation, h.WorkspaceID, h.ProjectID, h.MonitorID, h.OwnerUserID, h.MembershipID, h.RunID, lease.FencingToken).Scan(&raw)
	if err != nil {
		return StartSnapshot{}, databaseError(err)
	}
	var v startWire
	if json.Unmarshal(raw, &v) != nil || v.RunID != h.RunID || v.WorkspaceID != h.WorkspaceID || v.ProjectID != h.ProjectID || v.MonitorID != h.MonitorID || v.ConfigurationVersion != lease.ConfigurationVersion || v.ActivationGeneration != lease.ActivationGeneration || v.SchedulerEpoch != lease.SchedulerEpoch || v.FencingToken != lease.FencingToken || v.NamespaceIncarnation == "" || v.SupervisorIncarnation == "" || v.TimeoutSeconds < 1 || v.TimeoutSeconds > 30 || v.ExpectedStatus < 200 || v.ExpectedStatus > 599 || v.IntervalSeconds < 60 || len(v.URL) == 0 || len(v.URL) > 2048 || !utf8.ValidString(v.URL) || v.StartedAt.IsZero() || v.PermitDeadline.Before(v.StartedAt.Add(time.Duration(v.TimeoutSeconds)*time.Second)) || v.PermitDeadline.After(v.StartedAt.Add(35*time.Second)) {
		return StartSnapshot{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return StartSnapshot{}, ErrCommitUnknown
	}
	return StartSnapshot{input: &startInput{wire: v}}, nil
}
