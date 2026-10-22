//go:build schedulerfixture && linux

package supervisorfixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/credentials"
	"github.com/rjgczp/devpilot/internal/monitoring/handoff"
	"github.com/rjgczp/devpilot/internal/scheduler"
)

// VerifiedStop is constructible only after the independently pinned signature
// AND corresponding committed physical proofs verify. It is not a launch token.
type VerifiedStop struct {
	raw     []byte
	lineage handoff.Lineage
	nonce   string
	digest  [32]byte
}

func (v VerifiedStop) ReceiptID() string { return v.nonce }
func (v VerifiedStop) Digest() []byte    { return append([]byte(nil), v.digest[:]...) }

type processStop struct {
	Event        string          `json:"event"`
	Lineage      handoff.Lineage `json:"lineage"`
	NetNamespace string          `json:"net_namespace"`
	PIDNamespace string          `json:"pid_namespace"`
	PIDs         []struct {
		PID        int    `json:"pid"`
		StartTicks string `json:"start_ticks"`
		Exited     bool   `json:"pidfd_exited"`
	} `json:"pids"`
	Remaining      []int `json:"remaining_pids"`
	LaunchConsumed bool  `json:"launch_consumed"`
	At             int64 `json:"at_ns"`
	Started        int64 `json:"started_monotonic_ns"`
	Finished       int64 `json:"finished_monotonic_ns"`
}
type retiredStop struct {
	Event        string          `json:"event"`
	PIDNamespace string          `json:"pid_namespace"`
	Lineage      handoff.Lineage `json:"lineage"`
}

func decodeProof(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > 32768 || !credentials.ValidJSON(raw) || uniqueJSON(json.NewDecoder(bytes.NewReader(raw)), 0) != nil {
		return ErrDenied
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	d.UseNumber()
	if d.Decode(v) != nil {
		return ErrDenied
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrDenied
	}
	return nil
}
func proofDigest(raw []byte) string {
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return ""
	}
	b, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// VerifyCommittedStopBundle runs in the trusted Supervisor, after its private
// journal has fsynced the proofs and signed receipt, before acquiring any DB
// connection. Neither Worker files nor signature-only evidence is accepted.
func VerifyCommittedStopBundle(raw, publicPEM, bundle []byte, expected handoff.Lineage) (VerifiedStop, error) {
	denied := VerifiedStop{}
	if VerifyStopReceipt(raw, publicPEM, expected) != nil {
		return denied, ErrDenied
	}
	var receipt StopReceipt
	if Decode(raw, &receipt) != nil {
		return denied, ErrDenied
	}
	p := receipt.Payload
	var b struct {
		Network json.RawMessage `json:"network"`
		Process json.RawMessage `json:"process"`
		Retired json.RawMessage `json:"retired"`
	}
	if decodeProof(bundle, &b) != nil || proofDigest(b.Network) != p.NetworkSHA256 || proofDigest(b.Process) != p.ProcessSHA256 || proofDigest(b.Retired) != p.RetireSHA256 {
		return denied, ErrDenied
	}
	var network []NetworkStop
	var process processStop
	var retired retiredStop
	if decodeProof(b.Network, &network) != nil || len(network) != 2 || decodeProof(b.Process, &process) != nil || decodeProof(b.Retired, &retired) != nil {
		return denied, ErrDenied
	}
	roles := map[string]bool{}
	namespaces := map[string]bool{}
	for _, n := range network {
		if n.Lineage != expected || (n.Role != "run" && n.Role != "gateway") || roles[n.Role] || namespaces[n.Namespace] || n.Namespace == "" || !n.LinkRemoved || !n.ConntrackEmpty || n.StoppedAt.IsZero() || n.StoppedAt.UnixNano() > process.At {
			return denied, ErrDenied
		}
		if n.Role == "run" && n.Namespace != p.NetNamespace {
			return denied, ErrDenied
		}
		roles[n.Role] = true
		namespaces[n.Namespace] = true
	}
	if process.Event != "processes_terminated" || process.Lineage != expected || process.NetNamespace != p.NetNamespace || process.PIDNamespace != p.PIDNamespace || !process.LaunchConsumed || len(process.Remaining) != 0 || process.Remaining == nil || len(process.PIDs) != p.ProcessCount || process.Started <= 0 || process.Finished < process.Started || process.Finished-process.Started > int64(2*time.Second) || process.At <= 0 {
		return denied, ErrDenied
	}
	seen := map[int]bool{}
	for _, pid := range process.PIDs {
		ticks, e := strconv.ParseUint(pid.StartTicks, 10, 64)
		if pid.PID <= 1 || seen[pid.PID] || !pid.Exited || e != nil || ticks == 0 || strconv.FormatUint(ticks, 10) != pid.StartTicks {
			return denied, ErrDenied
		}
		seen[pid.PID] = true
	}
	if retired.Event != "pid_namespace_retiring" || retired.Lineage != expected || retired.PIDNamespace != process.PIDNamespace {
		return denied, ErrDenied
	}
	return VerifiedStop{raw: append([]byte(nil), raw...), lineage: expected, nonce: p.ReceiptID, digest: sha256.Sum256(raw)}, nil
}

type Settlement struct {
	DB          scheduler.Database
	Incarnation string
}
type PhysicalOutcome struct {
	RunID      string     `json:"run_id"`
	ReceiptID  string     `json:"receipt_id"`
	State      string     `json:"state"`
	ReleasedAt *time.Time `json:"released_at"`
}

var ErrSettlementUnknown = errors.New("physical settlement acknowledgement unknown")

// Accept never performs process/network I/O or retries an uncertain Commit.
func (s Settlement) Accept(ctx context.Context, v VerifiedStop) (PhysicalOutcome, error) {
	if s.DB == nil || len(v.raw) == 0 || !v.lineage.Valid() || v.lineage.SupervisorIncarnation != s.Incarnation {
		return PhysicalOutcome{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return PhysicalOutcome{}, ErrDenied
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		_ = tx.Rollback(c)
	}()
	l := v.lineage
	var raw []byte
	e = tx.QueryRow(ctx, `SELECT scheduler_private.accept_termination($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bytea)`, s.Incarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, v.raw).Scan(&raw)
	if e != nil {
		return PhysicalOutcome{}, e
	}
	var out PhysicalOutcome
	if json.Unmarshal(raw, &out) != nil || out.RunID != l.RunID || out.ReceiptID != v.nonce || out.State != "consumed" || out.ReleasedAt == nil {
		return PhysicalOutcome{}, ErrDenied
	}
	if tx.Commit(ctx) != nil {
		return PhysicalOutcome{}, ErrSettlementUnknown
	}
	return out, nil
}
func (s Settlement) Reconcile(ctx context.Context, v VerifiedStop) (PhysicalOutcome, error) {
	if s.DB == nil || len(v.raw) == 0 || !v.lineage.Valid() || v.lineage.SupervisorIncarnation != s.Incarnation {
		return PhysicalOutcome{}, ErrDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	l := v.lineage
	var raw []byte
	if e := s.DB.QueryRow(ctx, `SELECT scheduler_private.reconcile_termination($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::bytea)`, s.Incarnation, l.WorkspaceID, l.ProjectID, l.MonitorID, l.RunID, v.nonce, v.digest[:]).Scan(&raw); e != nil {
		return PhysicalOutcome{}, e
	}
	var out PhysicalOutcome
	if json.Unmarshal(raw, &out) != nil || out.RunID != l.RunID || out.ReceiptID != v.nonce || (out.State != "consumed" && out.State != "pending") || (out.State == "consumed") != (out.ReleasedAt != nil) {
		return PhysicalOutcome{}, ErrDenied
	}
	return out, nil
}
