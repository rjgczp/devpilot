package workspace

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

type InvitationError string

func (e InvitationError) Error() string { return string(e) }

const (
	ErrInviteeUnavailable   InvitationError = "invitee_unavailable"
	ErrInvitationForbidden  InvitationError = "invitation_forbidden"
	ErrInvitationNotFound   InvitationError = "invitation_not_found"
	ErrInvitationPending    InvitationError = "invitation_pending"
	ErrInvitationLimit      InvitationError = "invitation_limit_reached"
	ErrInvitationNotPending InvitationError = "invitation_not_pending"
	ErrInvitationExpired    InvitationError = "invitation_expired"
	ErrAlreadyMember        InvitationError = "already_member"
)

type OwnerInvitation struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	InviteeUserID   string    `json:"invitee_user_id"`
	CreatedByUserID string    `json:"created_by_user_id"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}
type RecipientInvitation struct {
	ID            string    `json:"id"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName *string   `json:"workspace_name"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ExpiresAt     time.Time `json:"expires_at"`
}
type InvitationPage struct {
	Data       []any `json:"data"`
	Pagination struct {
		NextAfter *string `json:"next_after"`
	} `json:"pagination"`
}
type Acceptance struct {
	InvitationID string    `json:"invitation_id"`
	WorkspaceID  string    `json:"workspace_id"`
	MembershipID string    `json:"membership_id"`
	Role         string    `json:"role"`
	JoinedAt     time.Time `json:"joined_at"`
}
type InvitationRepository interface {
	CreateInvitation(context.Context, session.Digest, string, string) (OwnerInvitation, error)
	ListInvitations(context.Context, session.Digest, string, bool, int, string, string) (InvitationPage, error)
	ActOnInvitation(context.Context, session.Digest, string, string) (Acceptance, error)
}

type invitationWrite struct {
	tx               pgx.Tx
	uid, wid, target string
	targetProtected  bool
}

func uuidSelector(s string) any {
	if validUUID(s) {
		return s
	}
	return nil
}

// Routing reads carry no authorization. All business predicates are rechecked
// after protective waits. User locks, including creation's target, come first.
func (s Store) beginInvitation(ctx context.Context, d session.Digest, wid, target, iid, action string) (*invitationWrite, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, classify(ctx, err)
	}
	b := &invitationWrite{tx: tx, wid: wid, target: target}
	fail := func(err error) (*invitationWrite, error) {
		tx.Rollback(ctx)
		if errors.Is(err, session.ErrInvalid) {
			return nil, session.ErrInvalid
		}
		return nil, classify(ctx, err)
	}
	if err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1`, d[:]).Scan(&b.uid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fail(session.ErrInvalid)
		}
		return fail(err)
	}
	lockTarget := false
	if action == "create" {
		// Do not inspect global target users for actors without even candidate Owner
		// scope. This read never authorizes a write or precedes final actor checks.
		var candidate bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 AND role='owner')`, uuidSelector(wid), b.uid).Scan(&candidate)
		if err != nil {
			return fail(err)
		}
		lockTarget = candidate && validUUID(target) && target != b.uid
	} else {
		err = tx.QueryRow(ctx, `SELECT i.workspace_id::text,i.invitee_user_id::text FROM workspace_invitations i
   WHERE i.id=$1 AND (($3<>'revoke' AND i.invitee_user_id=$2) OR ($3='revoke' AND EXISTS(
    SELECT 1 FROM workspace_members a WHERE a.workspace_id=i.workspace_id AND a.user_id=$2 AND a.role='owner')))`, uuidSelector(iid), b.uid, action).Scan(&b.wid, &b.target)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fail(err)
		}
	}
	ids := []string{b.uid}
	if lockTarget {
		ids = append(ids, target)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, id).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) && id != b.uid {
			continue
		}
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fail(session.ErrInvalid)
			}
			return fail(err)
		}
		if id == b.uid && status != "active" {
			return fail(session.ErrInvalid)
		}
		if id == target {
			b.targetProtected = true
		}
	}
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash=$1 AND user_id=$2 FOR SHARE`, d[:], b.uid).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && revoked {
		return fail(session.ErrInvalid)
	}
	if err != nil {
		return fail(err)
	}
	var locked string
	err = tx.QueryRow(ctx, `SELECT w.id::text FROM workspaces w WHERE w.id=$1 FOR NO KEY UPDATE OF w`, uuidSelector(b.wid)).Scan(&locked)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fail(err)
	}
	rows, err := tx.Query(ctx, `SELECT user_id::text FROM workspace_members WHERE workspace_id=$1 AND (user_id=$2 OR user_id=$3) ORDER BY user_id FOR UPDATE`, uuidSelector(b.wid), b.uid, uuidSelector(b.target))
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return fail(err)
	}
	rows, err = tx.Query(ctx, `SELECT id::text FROM workspace_invitations WHERE workspace_id=$1 AND (($4='create' AND invitee_user_id=$2 AND status='pending') OR ($4<>'create' AND id=$3)) ORDER BY id FOR UPDATE`, uuidSelector(b.wid), uuidSelector(b.target), uuidSelector(iid), action)
	if err != nil {
		return fail(err)
	}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return fail(err)
	}
	return b, nil
}

type invitationState struct {
	now                            time.Time
	role, targetStatus, id, status string
	owners, pending                int
	member                         bool
	expiry                         time.Time
}

func (b *invitationWrite) gate(ctx context.Context, d session.Digest, iid, action string) (invitationState, error) {
	var v invitationState
	err := b.tx.QueryRow(ctx, `WITH live_identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND u.id=$2 AND u.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp())
 SELECT statement_timestamp(),COALESCE(a.role,''),COALESCE(t.status,''),
 (SELECT count(*) FROM workspace_members o WHERE o.workspace_id=w.id AND o.role='owner'),
 EXISTS(SELECT 1 FROM workspace_members m WHERE m.workspace_id=w.id AND m.user_id=$4),
 COALESCE(inv.id::text,''),COALESCE(inv.status,''),COALESCE(inv.expires_at,statement_timestamp()),
 (SELECT count(*) FROM workspace_invitations p WHERE p.workspace_id=w.id AND p.status='pending' AND p.expires_at>statement_timestamp())
 FROM live_identity live LEFT JOIN workspaces w ON w.id=$3
 LEFT JOIN workspace_members a ON a.workspace_id=w.id AND a.user_id=live.id
 LEFT JOIN users t ON t.id=$4 AND $6='create' AND a.role='owner'
 LEFT JOIN workspace_invitations inv ON inv.workspace_id=w.id AND (($6='create' AND inv.invitee_user_id=$4 AND inv.status='pending') OR
 ($6<>'create' AND inv.id=$5 AND (($6='revoke' AND a.role='owner') OR ($6<>'revoke' AND inv.invitee_user_id=live.id))))`, d[:], b.uid, uuidSelector(b.wid), uuidSelector(b.target), uuidSelector(iid), action).Scan(&v.now, &v.role, &v.targetStatus, &v.owners, &v.member, &v.id, &v.status, &v.expiry, &v.pending)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, session.ErrInvalid
	}
	if err != nil {
		return v, classify(ctx, err)
	}
	return v, nil
}
func (b *invitationWrite) expire(ctx context.Context, id string) error {
	tag, err := b.tx.Exec(ctx, `UPDATE workspace_invitations SET status='expired',updated_at=expires_at WHERE id=$1 AND workspace_id=$2 AND status='pending'`, id, b.wid)
	if err != nil {
		return classify(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return user.ErrInternal
	}
	return nil
}

// commitOutcome deliberately commits lazy expiry before returning a business
// conflict. Never funnel such conflicts through the rollback-only error path.
func (b *invitationWrite) commitOutcome(ctx context.Context, outcome error) error {
	if ctx.Err() != nil {
		return classify(ctx, ctx.Err())
	}
	if err := b.tx.Commit(ctx); err != nil {
		return classify(ctx, err)
	}
	return outcome
}
func (s Store) CreateInvitation(ctx context.Context, d session.Digest, wid, target string) (OwnerInvitation, error) {
	var out OwnerInvitation
	b, err := s.beginInvitation(ctx, d, wid, target, "", "create")
	if err != nil {
		return out, err
	}
	defer b.tx.Rollback(ctx)
	v, err := b.gate(ctx, d, "", "create")
	if err != nil {
		return out, err
	}
	if v.role == "" {
		return out, ErrNotFound
	}
	if v.role != "owner" {
		return out, ErrInvitationForbidden
	}
	if v.owners == 0 {
		return out, user.ErrInternal
	}
	if !b.targetProtected || v.targetStatus != "active" || target == b.uid || v.member {
		return out, ErrInviteeUnavailable
	}
	if v.id != "" && v.expiry.After(v.now) {
		return out, ErrInvitationPending
	}
	// Expired pair is materialized even if capacity prevents replacement; this
	// conflict must confirm Commit. Invalid target/authority above never writes.
	if v.id != "" {
		if err = b.expire(ctx, v.id); err != nil {
			return out, err
		}
	}
	if v.pending >= 100 {
		return out, b.commitOutcome(ctx, ErrInvitationLimit)
	}
	err = b.tx.QueryRow(ctx, `INSERT INTO workspace_invitations(workspace_id,invitee_user_id,created_by_user_id,created_at,updated_at,expires_at)
 VALUES($1,$2,$3,$4,$4,$4::timestamptz+interval '168 hours') RETURNING id::text,workspace_id::text,invitee_user_id::text,created_by_user_id::text,status,created_at,updated_at,expires_at`, wid, target, b.uid, v.now).Scan(&out.ID, &out.WorkspaceID, &out.InviteeUserID, &out.CreatedByUserID, &out.Status, &out.CreatedAt, &out.UpdatedAt, &out.ExpiresAt)
	if err != nil {
		return OwnerInvitation{}, classify(ctx, err)
	}
	if err = b.commitOutcome(ctx, nil); err != nil {
		return OwnerInvitation{}, err
	}
	out.CreatedAt = out.CreatedAt.UTC()
	out.UpdatedAt = out.UpdatedAt.UTC()
	out.ExpiresAt = out.ExpiresAt.UTC()
	return out, nil
}
func (s Store) ActOnInvitation(ctx context.Context, d session.Digest, iid, action string) (Acceptance, error) {
	var out Acceptance
	if action != "accept" && action != "decline" && action != "revoke" {
		return out, user.ErrInternal
	}
	b, err := s.beginInvitation(ctx, d, "", "", iid, action)
	if err != nil {
		return out, err
	}
	defer b.tx.Rollback(ctx)
	v, err := b.gate(ctx, d, iid, action)
	if err != nil {
		return out, err
	}
	if v.id == "" {
		return out, ErrInvitationNotFound
	}
	if v.owners == 0 {
		return out, user.ErrInternal
	}
	if v.status != "pending" {
		if v.status == "expired" {
			return out, ErrInvitationExpired
		}
		return out, ErrInvitationNotPending
	}
	if !v.expiry.After(v.now) {
		if err = b.expire(ctx, v.id); err != nil {
			return out, err
		}
		return out, b.commitOutcome(ctx, ErrInvitationExpired)
	}
	if action == "accept" && v.member {
		return out, ErrAlreadyMember
	}
	next := map[string]string{"accept": "accepted", "decline": "declined", "revoke": "revoked"}[action]
	tag, err := b.tx.Exec(ctx, `UPDATE workspace_invitations SET status=$3,updated_at=$4 WHERE id=$1 AND workspace_id=$2 AND status='pending'`, iid, b.wid, next, v.now)
	if err != nil {
		return out, classify(ctx, err)
	}
	if tag.RowsAffected() != 1 {
		return out, user.ErrInternal
	}
	if action == "accept" {
		out.InvitationID = iid
		out.WorkspaceID = b.wid
		out.Role = "member"
		err = b.tx.QueryRow(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role,created_at) VALUES($1,$2,'member',$3) RETURNING membership_id::text,created_at`, b.wid, b.uid, v.now).Scan(&out.MembershipID, &out.JoinedAt)
		if err != nil {
			return Acceptance{}, classify(ctx, err)
		}
		out.JoinedAt = out.JoinedAt.UTC()
	}
	if err = b.commitOutcome(ctx, nil); err != nil {
		return Acceptance{}, err
	}
	return out, nil
}

func (s Store) ListInvitations(ctx context.Context, d session.Digest, wid string, owner bool, limit int, after, filter string) (InvitationPage, error) {
	page := InvitationPage{Data: make([]any, 0)}
	if limit < 1 || limit > 100 || (after != "" && !validUUID(after)) || (filter != "pending" && filter != "all") {
		return page, user.ErrInternal
	}
	rows, err := s.Pool.Query(ctx, identitySQL+`SELECT live.id::text,COALESCE(a.role,''),p.id,p.workspace_id,p.invitee_user_id,p.created_by_user_id,p.status,p.created_at,p.updated_at,p.expires_at,p.name
 FROM identity live LEFT JOIN workspace_members a ON $3 AND a.workspace_id=$2 AND a.user_id=live.id
 LEFT JOIN LATERAL (
 SELECT i.id::text,i.workspace_id::text,i.invitee_user_id::text,i.created_by_user_id::text,
 CASE WHEN i.status='pending' AND i.expires_at<=statement_timestamp() THEN 'expired' ELSE i.status END status,i.created_at,
 CASE WHEN i.status='pending' AND i.expires_at<=statement_timestamp() THEN i.expires_at ELSE i.updated_at END updated_at,i.expires_at,
 CASE WHEN NOT $3 AND i.status='pending' AND i.expires_at>statement_timestamp() THEN w.name END name
 FROM workspace_invitations i JOIN workspaces w ON w.id=i.workspace_id
 WHERE (($3 AND a.role='owner' AND i.workspace_id=$2) OR (NOT $3 AND i.invitee_user_id=live.id))
 AND ($4::uuid IS NULL OR i.id>$4) AND ($5='all' OR (i.status='pending' AND i.expires_at>statement_timestamp()))
 ORDER BY i.id LIMIT $6) p ON true ORDER BY p.id::uuid`, d[:], uuidSelector(wid), owner, uuidSelector(after), filter, limit+1)
	if err != nil {
		return page, classify(ctx, err)
	}
	defer rows.Close()
	valid := false
	role := ""
	ids := []string{}
	for rows.Next() {
		var uid string
		var id, w, i, c, state, name *string
		var created, updated, expiry *time.Time
		if err = rows.Scan(&uid, &role, &id, &w, &i, &c, &state, &created, &updated, &expiry, &name); err != nil {
			return page, classify(ctx, err)
		}
		valid = true
		if id == nil {
			continue
		}
		ids = append(ids, *id)
		if owner {
			page.Data = append(page.Data, OwnerInvitation{*id, *w, *i, *c, *state, created.UTC(), updated.UTC(), expiry.UTC()})
		} else {
			page.Data = append(page.Data, RecipientInvitation{*id, *w, name, *state, created.UTC(), updated.UTC(), expiry.UTC()})
		}
	}
	if err = rows.Err(); err != nil {
		return page, classify(ctx, err)
	}
	if !valid {
		return page, session.ErrInvalid
	}
	if owner && role == "" {
		return page, ErrNotFound
	}
	if owner && role != "owner" {
		return page, ErrInvitationForbidden
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := ids[limit-1]
		page.Pagination.NextAfter = &last
	}
	return page, nil
}
