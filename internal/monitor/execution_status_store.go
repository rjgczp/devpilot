package monitor

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func (s Store) ExecutionStatus(ctx context.Context, d session.Digest, wid, pid, mid string) (ExecutionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var a ExecutionStatus
	var decision string
	var raw []byte
	// The SQL function performs identity, scope and every projection in ONE SQL
	// statement/snapshot. No target networking or operational state is inferred.
	err := s.Pool.QueryRow(ctx, `SELECT decision,data FROM scheduler_private.execution_status($1,$2::uuid,$3::uuid,$4::uuid)`, d[:], selector(wid), selector(pid), selector(mid)).Scan(&decision, &raw)
	if err != nil {
		return a, classify(ctx, err)
	}
	switch decision {
	case "unauthenticated":
		return a, session.ErrInvalid
	case "not_found":
		return a, ErrNotFound
	case "ok":
	default:
		return a, user.ErrInternal
	}
	if json.Unmarshal(raw, &a) != nil || a.MonitorID == "" {
		return a, user.ErrInternal
	}
	a.normalizeUTC()
	return a, nil
}
