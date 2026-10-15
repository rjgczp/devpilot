// Package workspace owns tenant-scoped access and atomic workspace creation.
package workspace

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

var ErrNotFound = errors.New("workspace not found")

type Public struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Page struct {
	Data       []Public `json:"data"`
	Pagination struct {
		NextAfter *string `json:"next_after"`
	} `json:"pagination"`
}

// Database is the narrow pgx surface also used for controlled transaction-fault
// tests. No unauthenticated resource lookup is exposed by Store.
type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Store struct{ Pool Database }

func classify(ctx context.Context, err error) error {
	err = user.Classify(ctx, err)
	// Email occupancy is a registration contract, never a workspace conflict.
	if errors.Is(err, user.ErrConflict) {
		return user.ErrInternal
	}
	return err
}

// Create never retries: a failed Commit can still have committed both rows.
// The caller bounds the WHOLE transaction, including pool and lock waits.
func (s Store) Create(ctx context.Context, digest session.Digest, name string) (Public, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	// pgx closes the connection if a canceled context prevents rollback, rather
	// than returning a connection with an open transaction to the pool.
	defer tx.Rollback(ctx)
	var uid, status string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1`, digest[:]).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	// Lock order is user THEN session. KEY SHARE would not protect status.
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, uid).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if status != "active" {
		return Public{}, session.ErrInvalid
	}
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions
		WHERE token_hash=$1 AND user_id=$2 FOR SHARE`, digest[:], uid).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if revoked {
		return Public{}, session.ErrInvalid
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	// NEW statement after both locks: expiry must not use transaction-start or
	// the pre-wait statement timestamp. Locks serialize state, not time.
	var w Public
	err = tx.QueryRow(ctx, `INSERT INTO workspaces(name)
		SELECT $1 FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$2 AND u.id=$3 AND u.status='active'
		AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp()
		RETURNING id::text,name,created_at,updated_at`, name, digest[:], uid).
		Scan(&w.ID, &w.Name, &w.CreatedAt, &w.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, w.ID, uid)
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	if err = tx.Commit(ctx); err != nil {
		return Public{}, classify(ctx, err)
	}
	w.Role = "owner"
	w.CreatedAt, w.UpdatedAt = w.CreatedAt.UTC(), w.UpdatedAt.UTC()
	return w, nil
}

const identitySQL = `WITH identity AS MATERIALIZED (
	SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id
	WHERE s.token_hash=$1 AND s.revoked_at IS NULL
	AND s.expires_at>statement_timestamp() AND u.status='active'
) `

// Detail binds identity, membership and resource in ONE statement snapshot.
// Invalid UUIDs become NULL selectors, allowing live identity to win over 404.
func (s Store) Detail(ctx context.Context, digest session.Digest, id string) (Public, error) {
	var selector any
	if validUUID(id) {
		selector = id
	}
	var uid string
	var wid, name, role *string
	var created, updated *time.Time
	err := s.Pool.QueryRow(ctx, identitySQL+`SELECT i.id::text,w.id::text,w.name,m.role,w.created_at,w.updated_at
		FROM identity i
		LEFT JOIN workspace_members m ON m.user_id=i.id AND m.workspace_id=$2::uuid
		LEFT JOIN workspaces w ON w.id=m.workspace_id`, digest[:], selector).
		Scan(&uid, &wid, &name, &role, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if wid == nil {
		return Public{}, ErrNotFound
	}
	return Public{*wid, *name, *role, created.UTC(), updated.UTC()}, nil
}

func (s Store) List(ctx context.Context, digest session.Digest, limit int, after string) (Page, error) {
	// Defensive bounds also protect non-HTTP callers.
	if limit < 1 || limit > 100 || (after != "" && !validUUID(after)) {
		return Page{}, user.ErrInternal
	}
	var selector any
	if after != "" {
		selector = after
	}
	rows, err := s.Pool.Query(ctx, identitySQL+`SELECT i.id::text,p.id,p.name,p.role,p.created_at,p.updated_at
		FROM identity i LEFT JOIN LATERAL (
			SELECT w.id,w.name,m.role,w.created_at,w.updated_at
			FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id
			WHERE m.user_id=i.id AND ($2::uuid IS NULL OR w.id>$2::uuid)
			ORDER BY w.id LIMIT $3
		) p ON true ORDER BY p.id`, digest[:], selector, limit+1)
	if err != nil {
		return Page{}, classify(ctx, err)
	}
	defer rows.Close()
	page := Page{Data: make([]Public, 0)}
	valid := false
	for rows.Next() {
		var uid string
		var id, name, role *string
		var created, updated *time.Time
		if err := rows.Scan(&uid, &id, &name, &role, &created, &updated); err != nil {
			return Page{}, classify(ctx, err)
		}
		valid = true
		if id != nil {
			page.Data = append(page.Data, Public{*id, *name, *role, created.UTC(), updated.UTC()})
		}
	}
	if err := rows.Err(); err != nil {
		return Page{}, classify(ctx, err)
	}
	if !valid {
		return Page{}, session.ErrInvalid
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := page.Data[limit-1].ID
		page.Pagination.NextAfter = &last
	}
	return page, nil
}
