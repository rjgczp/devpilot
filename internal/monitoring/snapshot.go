// Package monitoring contains a dormant single-execution kernel. It has no
// production caller, public trigger, scheduler, worker or activation state.
package monitoring

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/monitor"
)

// Scope is always a complete immutable resource path, never a bare monitor ID.
type Scope struct{ WorkspaceID, ProjectID, MonitorID string }

func uuid(s string) bool {
	if len(s) != 36 || s == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range []byte(s) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func (s Scope) valid() bool { return uuid(s.WorkspaceID) && uuid(s.ProjectID) && uuid(s.MonitorID) }

// Snapshot has no exported fields or URL getter. Copying it cannot change a
// previously issued configuration. All fmt and JSON projections omit secrets.
type Snapshot struct {
	scope                                Scope
	name, target, method                 string
	interval, timeout, expected, version int64
}

func (s Snapshot) Scope() Scope               { return s.scope }
func (s Snapshot) Version() string            { return strconv.FormatInt(s.version, 10) }
func (s Snapshot) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, "<monitor snapshot>") }
func (s Snapshot) valid() bool {
	return s.scope.valid() && s.version > 0 && monitor.ValidStoredConfiguration(s.name, s.target, s.method, s.interval, s.timeout, s.expected)
}

var (
	ErrSnapshotUnavailable  = errors.New("snapshot_unavailable")
	ErrInvalidConfiguration = errors.New("invalid_configuration")
	ErrResourceLimited      = errors.New("resource_limited")
)

// SystemDatabase is supplied only by a trusted controller with its own database
// credentials. A user cookie, asserted role or scope string is not that identity.
type SystemDatabase interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type SnapshotReader struct{ Pool SystemDatabase }

const snapshotSQL = `SELECT m.workspace_id::text,m.project_id::text,m.id::text,
 m.name,m.url,m.method,m.interval_seconds,m.timeout_seconds,m.expected_status,m.version
 FROM workspaces w JOIN projects p ON p.workspace_id=w.id
 JOIN http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id
 WHERE w.id=$1::uuid AND p.id=$2::uuid AND m.id=$3::uuid`

// Read uses one statement snapshot and releases Rows/the pool connection before
// returning. No user/session lookup, transaction or row lock spans networking.
func (r SnapshotReader) Read(ctx context.Context, scope Scope) (Snapshot, error) {
	if !scope.valid() || r.Pool == nil {
		return Snapshot{}, ErrSnapshotUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rows, err := r.Pool.Query(ctx, snapshotSQL, scope.WorkspaceID, scope.ProjectID, scope.MonitorID)
	if err != nil {
		return Snapshot{}, ErrSnapshotUnavailable
	}
	defer rows.Close()
	var s Snapshot
	if !rows.Next() {
		return s, ErrSnapshotUnavailable
	}
	if rows.Scan(&s.scope.WorkspaceID, &s.scope.ProjectID, &s.scope.MonitorID, &s.name, &s.target, &s.method, &s.interval, &s.timeout, &s.expected, &s.version) != nil {
		return Snapshot{}, ErrSnapshotUnavailable
	}
	if rows.Next() || rows.Err() != nil || ctx.Err() != nil || s.scope != scope {
		return Snapshot{}, ErrSnapshotUnavailable
	}
	if !s.valid() {
		return Snapshot{}, ErrInvalidConfiguration
	}
	return s, nil
}
