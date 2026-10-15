package workspace

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

var (
	ErrPreconditionRequired = errors.New("membership precondition required")
	ErrPreconditionFailed   = errors.New("membership precondition failed")
	ErrMemberForbidden      = errors.New("membership operation forbidden")
	ErrMemberNotFound       = errors.New("member not found")
	ErrLastOwner            = errors.New("workspace must retain owner")
)

// DeleteMember never trusts path identity, retries, or looks up target users.
// All supported membership writers must lock User -> Session -> Workspace ->
// Membership. The Workspace lock serializes the cross-row owner invariant.
func (s Store) DeleteMember(ctx context.Context, digest session.Digest, wid, target, condition string) error {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return classify(ctx, err)
	}
	defer tx.Rollback(ctx)
	var uid, status string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1`, digest[:]).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.ErrInvalid
	}
	if err != nil {
		return classify(ctx, err)
	}
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, uid).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.ErrInvalid
	}
	if err != nil {
		return classify(ctx, err)
	}
	if status != "active" {
		return session.ErrInvalid
	}
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash=$1 AND user_id=$2 FOR SHARE`, digest[:], uid).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.ErrInvalid
	}
	if err != nil {
		return classify(ctx, err)
	}
	if revoked {
		return session.ErrInvalid
	}
	var selector, targetID any
	if validUUID(wid) {
		selector = wid
	}
	if validUUID(target) {
		targetID = target
	}
	var lockedID string
	err = tx.QueryRow(ctx, `SELECT w.id::text FROM workspaces w
 WHERE w.id=$1::uuid AND EXISTS (
  SELECT 1 FROM workspace_members a WHERE a.workspace_id=w.id AND a.user_id=$2
 ) FOR NO KEY UPDATE OF w`, selector, uid).Scan(&lockedID)
	workspaceLocked := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return classify(ctx, err)
	}
	// Preliminary scope selection occurs AFTER the Workspace lock. It cannot
	// authorize a write: the fresh final gate below rechecks every predicate.
	// A Member targeting another user must not even inspect that target relation.
	var actorRole string
	inspectTarget := false
	if workspaceLocked {
		err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, selector, uid).Scan(&actorRole)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return classify(ctx, err)
		}
		inspectTarget = targetID != nil && (actorRole == "owner" || uid == target)
		var lockTarget any
		if inspectTarget {
			lockTarget = targetID
		}
		rows, e := tx.Query(ctx, `SELECT user_id::text FROM workspace_members
   WHERE workspace_id=$1 AND (user_id=$2 OR user_id=$3::uuid)
   ORDER BY user_id FOR UPDATE`, selector, uid, lockTarget)
		if e != nil {
			return classify(ctx, e)
		}
		// Drain all rows so every protective lock is acquired BEFORE gate statement.
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return classify(ctx, e)
		}
	}
	if ctx.Err() != nil {
		return classify(ctx, ctx.Err())
	}
	generation, expectedRole, valid := parseMemberCondition(condition)
	if condition != "" && !valid {
		return user.ErrInternal
	}
	var decision string
	var deleted int
	// A separate command gets a fresh post-wait snapshot AND statement time.
	// Locks protect state, not the clock. This statement is the expiry gate;
	// expiry after it may still commit. No further business locks are acquired.
	err = tx.QueryRow(ctx, memberDeleteGate, digest[:], uid, selector, targetID, workspaceLocked, inspectTarget, generation, expectedRole, condition != "").Scan(&decision, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return session.ErrInvalid
	}
	if err != nil {
		return classify(ctx, err)
	}
	switch decision {
	case "workspace_not_found":
		return ErrNotFound
	case "member_not_found":
		return ErrMemberNotFound
	case "membership_forbidden":
		return ErrMemberForbidden
	case "last_owner_required":
		return ErrLastOwner
	case "precondition_required":
		return ErrPreconditionRequired
	case "precondition_failed":
		return ErrPreconditionFailed
	case "ok":
		if deleted != 1 {
			return user.ErrInternal
		}
	default:
		return user.ErrInternal
	}
	if ctx.Err() != nil {
		return classify(ctx, ctx.Err())
	}
	if err = tx.Commit(ctx); err != nil {
		return classify(ctx, err)
	}
	return nil
}

const memberDeleteGate = `WITH live_identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND u.id=$2 AND u.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp()
), state AS MATERIALIZED (
 SELECT i.id,a.role actor_role,t.role target_role,t.membership_id target_generation,
  (SELECT count(*) FROM workspace_members o WHERE o.workspace_id=$3::uuid AND a.role IS NOT NULL AND o.role='owner') owners
 FROM live_identity i
 LEFT JOIN workspace_members a ON a.workspace_id=$3::uuid AND a.user_id=i.id AND $5
 LEFT JOIN workspace_members t ON t.workspace_id=$3::uuid AND t.user_id=$4::uuid AND $6 AND (a.role='owner' OR i.id=$4::uuid)
), decision AS MATERIALIZED (
 SELECT CASE
  WHEN actor_role IS NULL THEN 'workspace_not_found'
  WHEN $4::uuid IS NULL THEN 'member_not_found'
  WHEN id<>$4::uuid AND (actor_role<>'owner' OR NOT $6) THEN 'membership_forbidden'
  WHEN id<>$4::uuid AND target_role='owner' THEN 'membership_forbidden'
  WHEN NOT $9 THEN 'precondition_required'
  WHEN target_generation IS NULL OR target_generation::text<>$7 OR target_role<>$8 THEN 'precondition_failed'
  WHEN owners=0 THEN 'internal_error'
  WHEN id=$4::uuid AND actor_role='owner' AND owners<=1 THEN 'last_owner_required'
  ELSE 'ok' END result FROM state
), deleted AS (
 DELETE FROM workspace_members m USING decision d
 WHERE d.result='ok' AND m.workspace_id=$3::uuid AND m.user_id=$4::uuid AND m.membership_id::text=$7 AND m.role=$8
 RETURNING m.user_id
) SELECT result,(SELECT count(*) FROM deleted) FROM decision`
