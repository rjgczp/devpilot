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

const activationJSON = `jsonb_build_object('workspace_id',a.workspace_id,'project_id',a.project_id,'monitor_id',a.monitor_id,
 'configuration_version',m.version::text,'desired_state',a.desired_state,
 'activation_configuration_version',a.activation_configuration_version::text,'activation_generation',a.activation_generation::text,
 'next_due_at',a.next_due_at,'next_enable_allowed_at',a.next_enable_allowed_at,'last_transition_kind',a.last_transition_kind,
 'created_at',a.created_at,'updated_at',a.updated_at,'execution_state','not_reported','blocked_reason','execution_status_required')`

func decodeActivation(raw []byte) (Activation, error) {
	var a Activation
	if len(raw) == 0 || json.Unmarshal(raw, &a) != nil || a.MonitorID == "" {
		return a, user.ErrInternal
	}
	a.CreatedAt = a.CreatedAt.UTC()
	a.UpdatedAt = a.UpdatedAt.UTC()
	if a.NextEnableAllowedAt != nil {
		v := a.NextEnableAllowedAt.UTC()
		a.NextEnableAllowedAt = &v
	}
	return a, nil
}
func (s Store) ActivationDetail(ctx context.Context, d session.Digest, wid, pid, mid string) (Activation, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var uid string
	var raw []byte
	var monitorID *string
	err := s.Pool.QueryRow(ctx, identitySQL+`SELECT i.id::text,m.id::text,CASE WHEN a.monitor_id IS NOT NULL THEN `+activationJSON+` END
 FROM identity i LEFT JOIN workspace_members b ON b.user_id=i.id AND b.workspace_id=$2::uuid
 LEFT JOIN projects p ON p.workspace_id=b.workspace_id AND p.id=$3::uuid
 LEFT JOIN http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=$4::uuid
 LEFT JOIN monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id`, d[:], selector(wid), selector(pid), selector(mid)).Scan(&uid, &monitorID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, session.ErrInvalid
	}
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	if monitorID == nil {
		return Activation{}, ErrNotFound
	}
	return decodeActivation(raw)
}

// ActivationUpdate follows the same parent protection protocol as configuration
// writers. The final new command gates the actual DML after every lock wait.
func (s Store) ActivationUpdate(ctx context.Context, d session.Digest, wid, pid, mid string, input ActivationInput, tag string) (Activation, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if len(validateActivation(input)) != 0 {
		return Activation{}, user.ErrInternal
	}
	expectedID, v, g, ok := activationCondition(tag)
	if tag != "" && !ok {
		return Activation{}, user.ErrInternal
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	defer tx.Rollback(ctx)
	var uid, status string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1`, d[:]).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, session.ErrInvalid
	}
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, uid).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && status != "active" {
		return Activation{}, session.ErrInvalid
	}
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash=$1 AND user_id=$2 FOR SHARE`, d[:], uid).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && revoked {
		return Activation{}, session.ErrInvalid
	}
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	var locked string
	err = tx.QueryRow(ctx, `SELECT w.id::text FROM workspaces w WHERE w.id=$1::uuid AND EXISTS(SELECT 1 FROM workspace_members a WHERE a.workspace_id=w.id AND a.user_id=$2) FOR NO KEY UPDATE OF w`, selector(wid), uid).Scan(&locked)
	protected := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, classify(ctx, err)
	}

	if protected {
		var role string
		err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 ORDER BY user_id FOR UPDATE`, selector(wid), uid).Scan(&role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Activation{}, classify(ctx, err)
		}
		if role == "owner" {
			err = tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, selector(wid), selector(pid)).Scan(&locked)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Activation{}, classify(ctx, err)
			}
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT id::text FROM http_monitors WHERE workspace_id=$1 AND project_id=$2::uuid AND id=$3::uuid FOR UPDATE`, selector(wid), selector(pid), selector(mid)).Scan(&locked)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Activation{}, classify(ctx, err)
				}
				if err == nil {
					err = tx.QueryRow(ctx, `SELECT monitor_id::text FROM monitor_activation_states WHERE workspace_id=$1 AND project_id=$2::uuid AND monitor_id=$3::uuid FOR UPDATE`, selector(wid), selector(pid), selector(mid)).Scan(&locked)
					if err != nil {
						return Activation{}, classify(ctx, err)
					}
				}
			}
		}
	}

	if ctx.Err() != nil {
		return Activation{}, classify(ctx, ctx.Err())
	}
	var decision string
	var raw []byte
	var retry int
	err = tx.QueryRow(ctx, activationWriteSQL, d[:], uid, selector(wid), selector(pid), selector(mid), protected, tag != "", selector(expectedID), v, g, input.ConfigurationVersion, input.DesiredState).Scan(&decision, &raw, &retry)
	if errors.Is(err, pgx.ErrNoRows) {
		return Activation{}, session.ErrInvalid
	}
	if err != nil {
		return Activation{}, classify(ctx, err)
	}
	switch decision {
	case "not_found":
		return Activation{}, ErrNotFound
	case "forbidden":
		return Activation{}, ErrForbidden
	case "required":
		return Activation{}, ErrPreconditionRequired
	case "failed":
		return Activation{}, ErrPreconditionFailed
	case "limit":
		return Activation{}, ErrActivationLimit
	case "cooling":
		return Activation{}, ActivationRateLimit{RetryAfter: retry}
	case "ok":
	default:
		return Activation{}, user.ErrInternal
	}
	a, err := decodeActivation(raw)
	if err != nil {
		return Activation{}, err
	}
	if ctx.Err() != nil {
		return Activation{}, classify(ctx, ctx.Err())
	}
	if err = tx.Commit(ctx); err != nil {
		return Activation{}, classify(ctx, err)
	}
	return a, nil
}

const activationWriteSQL = `WITH live_identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND u.id=$2 AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp()
), state AS MATERIALIZED (
 SELECT i.id,b.role,p.id project_id,m.id monitor_id,m.version,a.activation_generation,a.desired_state,a.next_enable_allowed_at
 FROM live_identity i LEFT JOIN workspace_members b ON b.workspace_id=$3::uuid AND b.user_id=i.id AND $6
 LEFT JOIN projects p ON p.workspace_id=b.workspace_id AND p.id=$4::uuid
 LEFT JOIN http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=$5::uuid
 LEFT JOIN monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
), decision AS MATERIALIZED (
 SELECT *, CASE WHEN role IS NULL OR project_id IS NULL OR monitor_id IS NULL THEN 'not_found'
 WHEN role<>'owner' THEN 'forbidden'
 WHEN activation_generation IS NULL THEN 'internal'
 WHEN NOT $7 THEN 'required'
 WHEN monitor_id IS DISTINCT FROM $8::uuid OR version<>$9::bigint OR activation_generation<>$10::bigint OR version<>$11::bigint THEN 'failed'
 WHEN $12='enabled' AND desired_state<>'enabled' AND (SELECT count(*) FROM monitor_activation_states WHERE workspace_id=$3::uuid AND desired_state='enabled')>=100 THEN 'limit'
 WHEN $12='enabled' AND next_enable_allowed_at>statement_timestamp() THEN 'cooling'
 WHEN activation_generation=9223372036854775807 THEN 'internal'
 ELSE 'ok' END result FROM state
), changed AS (
 UPDATE monitor_activation_states a SET desired_state=$12,
 activation_configuration_version=CASE WHEN $12='enabled' THEN d.version ELSE NULL END,
 activation_generation=a.activation_generation+1,
 next_enable_allowed_at=CASE WHEN $12='enabled' THEN GREATEST(a.next_enable_allowed_at,statement_timestamp()+interval '60 seconds') ELSE a.next_enable_allowed_at END,
 last_transition_kind=CASE WHEN $12='enabled' THEN 'owner_enabled' ELSE 'owner_paused' END,
 last_actor_user_id=d.id,updated_at=GREATEST(a.updated_at,statement_timestamp())
 FROM decision d,http_monitors m WHERE d.result='ok' AND a.workspace_id=$3::uuid AND a.project_id=$4::uuid AND a.monitor_id=$5::uuid
 AND a.activation_generation=$10::bigint AND m.workspace_id=a.workspace_id AND m.project_id=a.project_id AND m.id=a.monitor_id AND m.version=$9::bigint
 RETURNING ` + activationJSON + ` AS data
) SELECT result,(SELECT data FROM changed),CASE WHEN result='cooling' THEN GREATEST(1,ceil(extract(epoch FROM (next_enable_allowed_at-statement_timestamp())))::integer) ELSE 0 END FROM decision`
