package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

var ErrRetentionCapacity = errors.New("retention capacity")

func (s Store) ExecutionAuthorize(ctx context.Context, d session.Digest, wid, pid, mid string, input ExecutionAuthorizationInput, tag string) (ExecutionAuthorization, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var a ExecutionAuthorization
	if len(validateExecutionAuthorization(input)) != 0 {
		return a, user.ErrInternal
	}
	tagID, v, g, ok := activationCondition(tag)
	if tag != "" && !ok {
		return a, user.ErrInternal
	}
	var expectedV, expectedG any
	if tag != "" {
		expectedV = v
		expectedG = g
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return a, classify(ctx, err)
	}
	defer tx.Rollback(ctx)
	var decision string
	var raw []byte
	var retry int
	err = tx.QueryRow(ctx, `SELECT decision,data,retry_after FROM scheduler_private.authorize($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::bigint,$7::bigint,$8::bigint,$9::integer)`, d[:], selector(wid), selector(pid), selector(mid), selector(tagID), expectedV, expectedG, input.ConfigurationVersion, input.AuthorizationSeconds).Scan(&decision, &raw, &retry)
	if err != nil {
		return a, classify(ctx, err)
	}
	switch decision {
	case "unauthenticated":
		return a, session.ErrInvalid
	case "not_found":
		return a, ErrNotFound
	case "forbidden":
		return a, ErrForbidden
	case "required":
		return a, ErrPreconditionRequired
	case "failed":
		return a, ErrPreconditionFailed
	case "limit":
		return a, ErrActivationLimit
	case "cooling":
		return a, ActivationRateLimit{RetryAfter: retry}
	case "retention_capacity":
		return a, ErrRetentionCapacity
	case "ok":
	default:
		return a, user.ErrInternal
	}
	if json.Unmarshal(raw, &a) != nil || a.MonitorID == "" {
		return a, user.ErrInternal
	}
	a.IssuedAt = a.IssuedAt.UTC()
	a.ExpiresAt = a.ExpiresAt.UTC()
	if err = tx.Commit(ctx); err != nil {
		return ExecutionAuthorization{}, classify(ctx, err)
	}
	return a, nil
}
