package workspace

import (
	"context"
	"time"

	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

// Member deliberately excludes account/email/session state.
type Member struct {
	UserID   string    `json:"user_id"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}
type MemberPage struct {
	Data       []Member `json:"data"`
	Pagination struct {
		NextAfter *string `json:"next_after"`
	} `json:"pagination"`
}

// ListMembers evaluates identity, caller membership and roster in ONE snapshot.
// The identity/authorization root survives empty pages, but never an invalid identity.
func (s Store) ListMembers(ctx context.Context, digest session.Digest, id string, limit int, after string) (MemberPage, error) {
	if limit < 1 || limit > 100 || (after != "" && !validUUID(after)) {
		return MemberPage{}, user.ErrInternal
	}
	var selector, cursor any
	if validUUID(id) {
		selector = id
	}
	if after != "" {
		cursor = after
	}
	rows, err := s.Pool.Query(ctx, identitySQL+`SELECT i.id::text,w.id::text,p.user_id,p.role,p.created_at
 FROM identity i
 LEFT JOIN workspace_members actor ON actor.user_id=i.id AND actor.workspace_id=$2::uuid
 LEFT JOIN workspaces w ON w.id=actor.workspace_id
 LEFT JOIN LATERAL (
  SELECT m.user_id::text,m.role,m.created_at FROM workspace_members m
  WHERE m.workspace_id=w.id AND ($3::uuid IS NULL OR m.user_id>$3::uuid)
  ORDER BY m.user_id LIMIT $4
 ) p ON true ORDER BY p.user_id::uuid`, digest[:], selector, cursor, limit+1)
	if err != nil {
		return MemberPage{}, classify(ctx, err)
	}
	defer rows.Close()
	page := MemberPage{Data: make([]Member, 0)}
	valid, authorized := false, false
	for rows.Next() {
		var actor string
		var wid, uid, role *string
		var joined *time.Time
		if err := rows.Scan(&actor, &wid, &uid, &role, &joined); err != nil {
			return MemberPage{}, classify(ctx, err)
		}
		valid = true
		authorized = wid != nil
		if uid != nil {
			page.Data = append(page.Data, Member{*uid, *role, joined.UTC()})
		}
	}
	if err := rows.Err(); err != nil {
		return MemberPage{}, classify(ctx, err)
	}
	if !valid {
		return MemberPage{}, session.ErrInvalid
	}
	if !authorized {
		return MemberPage{}, ErrNotFound
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := page.Data[limit-1].UserID
		page.Pagination.NextAfter = &last
	}
	return page, nil
}
