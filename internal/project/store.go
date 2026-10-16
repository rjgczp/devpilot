package project

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

var (
	ErrNotFound             = errors.New("project not found")
	ErrForbidden            = errors.New("project forbidden")
	ErrPreconditionRequired = errors.New("project precondition required")
	ErrPreconditionFailed   = errors.New("project precondition failed")
)

type Public struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Version     string    `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type Page struct {
	Data       []Public `json:"data"`
	Pagination struct {
		NextAfter *string `json:"next_after"`
	} `json:"pagination"`
}
type Database interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Store struct{ Pool Database }

func classify(ctx context.Context, err error) error {
	err = user.Classify(ctx, err)
	if errors.Is(err, user.ErrConflict) {
		return user.ErrInternal
	}
	return err
}

const identitySQL = `WITH identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id
 WHERE s.token_hash=$1 AND u.status='active' AND s.revoked_at IS NULL
 AND s.expires_at>statement_timestamp()
) `

func (s Store) Detail(ctx context.Context, d session.Digest, wid, pid string) (Public, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var uid string
	var id, w, n, desc, version *string
	var created, updated *time.Time
	err := s.Pool.QueryRow(ctx, identitySQL+`SELECT i.id::text,p.id::text,p.workspace_id::text,p.name,p.description,p.version::text,p.created_at,p.updated_at
 FROM identity i LEFT JOIN workspace_members m ON m.user_id=i.id AND m.workspace_id=$2::uuid
 LEFT JOIN projects p ON p.workspace_id=m.workspace_id AND p.id=$3::uuid`, d[:], selector(wid), selector(pid)).Scan(&uid, &id, &w, &n, &desc, &version, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if id == nil {
		return Public{}, ErrNotFound
	}
	return Public{*id, *w, *n, *desc, *version, created.UTC(), updated.UTC()}, nil
}
func (s Store) List(ctx context.Context, d session.Digest, wid string, limit int, after string) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if limit < 1 || limit > 100 || after != "" && !validUUID(after) {
		return Page{}, user.ErrInternal
	}
	rows, err := s.Pool.Query(ctx, identitySQL+`SELECT i.id::text,m.workspace_id::text,p.id::text,p.name,p.description,p.version::text,p.created_at,p.updated_at
 FROM identity i LEFT JOIN workspace_members m ON m.user_id=i.id AND m.workspace_id=$2::uuid
 LEFT JOIN LATERAL (SELECT p.* FROM projects p WHERE p.workspace_id=m.workspace_id AND ($3::uuid IS NULL OR p.id>$3::uuid) ORDER BY p.id LIMIT $4) p ON true ORDER BY p.id`, d[:], selector(wid), selector(after), limit+1)
	if err != nil {
		return Page{}, classify(ctx, err)
	}
	defer rows.Close()
	page := Page{Data: make([]Public, 0)}
	valid, scope := false, false
	for rows.Next() {
		var uid string
		var w, id, n, desc, version *string
		var created, updated *time.Time
		if err = rows.Scan(&uid, &w, &id, &n, &desc, &version, &created, &updated); err != nil {
			return Page{}, classify(ctx, err)
		}
		valid = true
		scope = w != nil
		if id != nil {
			page.Data = append(page.Data, Public{*id, *w, *n, *desc, *version, created.UTC(), updated.UTC()})
		}
	}
	if err = rows.Err(); err != nil {
		return Page{}, classify(ctx, err)
	}
	if !valid {
		return Page{}, session.ErrInvalid
	}
	if !scope {
		return Page{}, ErrNotFound
	}
	if len(page.Data) > limit {
		page.Data = page.Data[:limit]
		last := page.Data[limit-1].ID
		page.Pagination.NextAfter = &last
	}
	return page, nil
}

func (s Store) Create(ctx context.Context, d session.Digest, wid string, v Input) (Public, error) {
	return s.write(ctx, d, wid, "", v, "", true)
}

// Update is the only supported attribute mutation. Never refresh a caller's
// precondition or introduce id-only attribute writers in other business modules.
func (s Store) Update(ctx context.Context, d session.Digest, wid, pid string, v Input, tag string) (Public, error) {
	return s.write(ctx, d, wid, pid, v, tag, false)
}
func (s Store) write(ctx context.Context, d session.Digest, wid, pid string, v Input, tag string, create bool) (Public, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if len(validate(v)) != 0 || create && v.Name == nil || !create && v.Name == nil && v.Description == nil {
		return Public{}, user.ErrInternal
	}
	expectedID, expectedVersion, ok := condition(tag)
	if !create && tag != "" && !ok {
		return Public{}, user.ErrInternal
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	defer tx.Rollback(ctx)
	var uid, status string
	err = tx.QueryRow(ctx, `SELECT user_id::text FROM sessions WHERE token_hash=$1`, d[:]).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	err = tx.QueryRow(ctx, `SELECT status FROM users WHERE id=$1 FOR SHARE`, uid).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && status != "active" {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	var revoked bool
	err = tx.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM sessions WHERE token_hash=$1 AND user_id=$2 FOR SHARE`, d[:], uid).Scan(&revoked)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && revoked {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	var locked string
	err = tx.QueryRow(ctx, `SELECT w.id::text FROM workspaces w WHERE w.id=$1::uuid AND EXISTS(SELECT 1 FROM workspace_members a WHERE a.workspace_id=w.id AND a.user_id=$2) FOR NO KEY UPDATE OF w`, selector(wid), uid).Scan(&locked)
	workspaceLocked := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Public{}, classify(ctx, err)
	}
	role := ""
	if workspaceLocked {
		err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 ORDER BY user_id FOR UPDATE`, selector(wid), uid).Scan(&role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Public{}, classify(ctx, err)
		}
		if !create && role == "owner" {
			err = tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, selector(wid), selector(pid)).Scan(&locked)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Public{}, classify(ctx, err)
			}
		}
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	// Fresh command AFTER every protective wait; INSERT/UPDATE itself contains
	// the live identity/tenant/Owner gate. Locks protect state, never time.
	var decision string
	var id, w, n, desc, ver *string
	var created, updated *time.Time
	err = tx.QueryRow(ctx, writeSQL, d[:], uid, selector(wid), selector(pid), workspaceLocked, create, tag != "", selector(expectedID), expectedVersion, v.Name, v.Description).Scan(&decision, &id, &w, &n, &desc, &ver, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	switch decision {
	case "not_found":
		return Public{}, ErrNotFound
	case "forbidden":
		return Public{}, ErrForbidden
	case "required":
		return Public{}, ErrPreconditionRequired
	case "failed":
		return Public{}, ErrPreconditionFailed
	case "ok":
	default:
		return Public{}, user.ErrInternal
	}
	if id == nil {
		return Public{}, user.ErrInternal
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	if err = tx.Commit(ctx); err != nil {
		return Public{}, classify(ctx, err)
	}
	return Public{*id, *w, *n, *desc, *ver, created.UTC(), updated.UTC()}, nil
}

const writeSQL = `WITH live_identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND u.id=$2 AND u.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp()
), state AS MATERIALIZED (
 SELECT i.id,a.workspace_id,a.role,p.id project_id,p.version FROM live_identity i
 LEFT JOIN workspace_members a ON a.workspace_id=$3::uuid AND a.user_id=i.id AND $5
 LEFT JOIN projects p ON p.workspace_id=a.workspace_id AND p.id=$4::uuid
), decision AS MATERIALIZED (
 SELECT *, CASE WHEN role IS NULL THEN 'not_found'
 WHEN NOT $6 AND project_id IS NULL THEN 'not_found'
 WHEN role<>'owner' THEN 'forbidden'
 WHEN NOT $6 AND NOT $7 THEN 'required'
 WHEN NOT $6 AND (project_id IS DISTINCT FROM $8::uuid OR version<>$9::bigint) THEN 'failed'
 WHEN NOT $6 AND version=9223372036854775807 THEN 'internal'
 ELSE 'ok' END result FROM state
), inserted AS (
 INSERT INTO projects(workspace_id,name,description,created_by_user_id)
 SELECT workspace_id,$10::text,COALESCE($11::text,''),id FROM decision WHERE $6 AND result='ok'
 RETURNING id,workspace_id,name,description,version,created_at,updated_at
), changed AS (
 UPDATE projects p SET name=COALESCE($10::text,p.name),description=COALESCE($11::text,p.description),
 version=p.version+1,updated_at=GREATEST(p.created_at,p.updated_at,statement_timestamp())
 FROM decision d WHERE NOT $6 AND d.result='ok' AND p.workspace_id=$3::uuid AND p.id=$4::uuid AND p.version=$9::bigint
 AND d.role='owner' AND d.workspace_id=p.workspace_id AND d.project_id=p.id
 RETURNING p.id,p.workspace_id,p.name,p.description,p.version,p.created_at,p.updated_at
), output AS (SELECT * FROM inserted UNION ALL SELECT * FROM changed)
 SELECT d.result,o.id::text,o.workspace_id::text,o.name,o.description,o.version::text,o.created_at,o.updated_at
 FROM decision d LEFT JOIN output o ON true`
