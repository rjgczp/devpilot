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

var (
	ErrNotFound             = errors.New("monitor not found")
	ErrForbidden            = errors.New("monitor forbidden")
	ErrPreconditionRequired = errors.New("monitor precondition required")
	ErrPreconditionFailed   = errors.New("monitor precondition failed")
)

type Summary struct {
	ID              string    `json:"id"`
	WorkspaceID     string    `json:"workspace_id"`
	ProjectID       string    `json:"project_id"`
	Name            string    `json:"name"`
	Method          string    `json:"method"`
	IntervalSeconds int64     `json:"interval_seconds"`
	TimeoutSeconds  int64     `json:"timeout_seconds"`
	ExpectedStatus  int64     `json:"expected_status"`
	Version         string    `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}
type Public struct {
	Summary
	URL string `json:"url"`
}
type Page struct {
	Data       []Summary `json:"data"`
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

// Explicit JSON projection avoids both nullable LEFT JOIN field scanning and
// accidental leakage of provenance or URL in a collection query.
const summaryJSON = `jsonb_build_object('id',m.id,'workspace_id',m.workspace_id,'project_id',m.project_id,
 'name',m.name,'method',m.method,'interval_seconds',m.interval_seconds,'timeout_seconds',m.timeout_seconds,
 'expected_status',m.expected_status,'version',m.version::text,'created_at',m.created_at,'updated_at',m.updated_at)`
const publicJSON = summaryJSON + ` || jsonb_build_object('url',m.url)`

func decode(raw []byte) (Public, error) {
	var p Public
	if len(raw) == 0 || json.Unmarshal(raw, &p) != nil || p.ID == "" {
		return p, user.ErrInternal
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	return p, nil
}
func (s Store) Detail(ctx context.Context, d session.Digest, wid, pid, mid string) (Public, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var uid string
	var raw []byte
	err := s.Pool.QueryRow(ctx, identitySQL+`SELECT i.id::text,CASE WHEN m.id IS NOT NULL THEN `+publicJSON+` END
 FROM identity i LEFT JOIN workspace_members a ON a.user_id=i.id AND a.workspace_id=$2::uuid
 LEFT JOIN projects p ON p.workspace_id=a.workspace_id AND p.id=$3::uuid
 LEFT JOIN http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=$4::uuid`, d[:], selector(wid), selector(pid), selector(mid)).Scan(&uid, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Public{}, session.ErrInvalid
	}
	if err != nil {
		return Public{}, classify(ctx, err)
	}
	if len(raw) == 0 {
		return Public{}, ErrNotFound
	}
	return decode(raw)
}
func (s Store) List(ctx context.Context, d session.Digest, wid, pid string, limit int, after string) (Page, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if limit < 1 || limit > 100 || after != "" && !validUUID(after) {
		return Page{}, user.ErrInternal
	}
	rows, err := s.Pool.Query(ctx, identitySQL+`SELECT i.id::text,p.id::text,CASE WHEN m.id IS NOT NULL THEN `+summaryJSON+` END
 FROM identity i LEFT JOIN workspace_members a ON a.user_id=i.id AND a.workspace_id=$2::uuid
 LEFT JOIN projects p ON p.workspace_id=a.workspace_id AND p.id=$3::uuid
 LEFT JOIN LATERAL (SELECT m.* FROM http_monitors m WHERE m.workspace_id=p.workspace_id AND m.project_id=p.id
 AND ($4::uuid IS NULL OR m.id>$4::uuid) ORDER BY m.id LIMIT $5) m ON true ORDER BY m.id`, d[:], selector(wid), selector(pid), selector(after), limit+1)
	if err != nil {
		return Page{}, classify(ctx, err)
	}
	defer rows.Close()
	page := Page{Data: make([]Summary, 0)}
	valid, scope := false, false
	for rows.Next() {
		var uid string
		var parent *string
		var raw []byte
		if err = rows.Scan(&uid, &parent, &raw); err != nil {
			return Page{}, classify(ctx, err)
		}
		valid = true
		scope = parent != nil
		if len(raw) != 0 {
			p, e := decode(raw)
			if e != nil {
				return Page{}, e
			}
			page.Data = append(page.Data, p.Summary)
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
func (s Store) Create(ctx context.Context, d session.Digest, wid, pid string, v Input) (Public, error) {
	return s.write(ctx, d, wid, pid, "", v, "", true)
}

// Update never refreshes a caller's precondition, merges stale intent, or writes
// attributes using a bare ID. All business mutations go through this gate.
func (s Store) Update(ctx context.Context, d session.Digest, wid, pid, mid string, v Input, tag string) (Public, error) {
	return s.write(ctx, d, wid, pid, mid, v, tag, false)
}
func (s Store) write(ctx context.Context, d session.Digest, wid, pid, mid string, v Input, tag string, create bool) (Public, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	v = clean(v)
	if create {
		v = defaults(v)
	}
	if len(validate(v)) != 0 || create && (v.Name == nil || v.URL == nil) || empty(v) {
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
	protected := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Public{}, classify(ctx, err)
	}
	mergedValid := true
	if protected {
		var role string
		err = tx.QueryRow(ctx, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 ORDER BY user_id FOR UPDATE`, selector(wid), uid).Scan(&role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Public{}, classify(ctx, err)
		}
		if role == "owner" {
			err = tx.QueryRow(ctx, `SELECT id::text FROM projects WHERE workspace_id=$1 AND id=$2::uuid FOR UPDATE`, selector(wid), selector(pid)).Scan(&locked)
			parentLocked := err == nil
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return Public{}, classify(ctx, err)
			}
			if !create && parentLocked {
				var name, url, method string
				var interval, timeout, status int64
				err = tx.QueryRow(ctx, `SELECT id::text,name,url,method,interval_seconds,timeout_seconds,expected_status FROM http_monitors WHERE workspace_id=$1 AND project_id=$2::uuid AND id=$3::uuid FOR UPDATE`, selector(wid), selector(pid), selector(mid)).Scan(&locked, &name, &url, &method, &interval, &timeout, &status)
				if err == nil {
					merged := v
					if merged.Name == nil {
						merged.Name = &name
					}
					if merged.URL == nil {
						merged.URL = &url
					}
					if merged.Method == nil {
						merged.Method = &method
					}
					if merged.IntervalSeconds == nil {
						merged.IntervalSeconds = &interval
					}
					if merged.TimeoutSeconds == nil {
						merged.TimeoutSeconds = &timeout
					}
					if merged.ExpectedStatus == nil {
						merged.ExpectedStatus = &status
					}
					canonical, ok := canonicalURL(*merged.URL)
					mergedValid = len(validate(merged)) == 0 && ok && canonical == *merged.URL
				}
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return Public{}, classify(ctx, err)
				}
			}
		}
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	// A separate command after ALL protective waits refreshes statement time and
	// snapshot. Both actual DML arms carry live authority and complete scope.
	var decision string
	var raw []byte
	err = tx.QueryRow(ctx, writeSQL, d[:], uid, selector(wid), selector(pid), selector(mid), protected, create, tag != "", selector(expectedID), expectedVersion, v.Name, v.URL, v.Method, v.IntervalSeconds, v.TimeoutSeconds, v.ExpectedStatus, mergedValid).Scan(&decision, &raw)
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
	p, err := decode(raw)
	if err != nil {
		return Public{}, err
	}
	if ctx.Err() != nil {
		return Public{}, classify(ctx, ctx.Err())
	}
	if err = tx.Commit(ctx); err != nil {
		return Public{}, classify(ctx, err)
	}
	return p, nil
}

const writeSQL = `WITH live_identity AS MATERIALIZED (
 SELECT u.id FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND u.id=$2 AND u.status='active'
 AND s.revoked_at IS NULL AND s.expires_at>statement_timestamp()
), state AS MATERIALIZED (
 SELECT i.id,a.workspace_id,a.role,p.id project_id,m.id monitor_id,m.version,
 COALESCE($11::text,m.name) name,COALESCE($12::text,m.url) url,COALESCE($13::text,m.method) method,
 COALESCE($14::integer,m.interval_seconds) interval_seconds,COALESCE($15::integer,m.timeout_seconds) timeout_seconds,
 COALESCE($16::integer,m.expected_status) expected_status
 FROM live_identity i LEFT JOIN workspace_members a ON a.workspace_id=$3::uuid AND a.user_id=i.id AND $6
 LEFT JOIN projects p ON p.workspace_id=a.workspace_id AND p.id=$4::uuid
 LEFT JOIN http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=$5::uuid
), decision AS MATERIALIZED (
 SELECT *,CASE WHEN role IS NULL OR project_id IS NULL OR (NOT $7 AND monitor_id IS NULL) THEN 'not_found'
 WHEN role<>'owner' THEN 'forbidden'
 WHEN NOT $7 AND NOT $8 THEN 'required'
 WHEN NOT $7 AND (monitor_id IS DISTINCT FROM $9::uuid OR version<>$10::bigint) THEN 'failed'
 WHEN NOT $7 AND version=9223372036854775807 THEN 'internal'
 WHEN NOT $17::boolean THEN 'internal'
 WHEN method<>'GET' OR interval_seconds NOT IN(60,300,900,1800,3600) OR timeout_seconds NOT BETWEEN 1 AND 30
 OR timeout_seconds>=interval_seconds OR expected_status NOT BETWEEN 200 AND 599 THEN 'internal'
 ELSE 'ok' END result FROM state
), inserted AS (
 INSERT INTO http_monitors(workspace_id,project_id,name,url,method,interval_seconds,timeout_seconds,expected_status,created_by_user_id)
 SELECT workspace_id,project_id,name,url,method,interval_seconds,timeout_seconds,expected_status,id FROM decision WHERE $7 AND result='ok'
 RETURNING *
), changed AS (
 UPDATE http_monitors m SET name=d.name,url=d.url,method=d.method,interval_seconds=d.interval_seconds,
 timeout_seconds=d.timeout_seconds,expected_status=d.expected_status,version=m.version+1,
 updated_at=GREATEST(m.created_at,m.updated_at,statement_timestamp())
 FROM decision d WHERE NOT $7 AND d.result='ok' AND m.workspace_id=$3::uuid AND m.project_id=$4::uuid AND m.id=$5::uuid
 AND m.version=$10::bigint AND d.role='owner' AND d.workspace_id=m.workspace_id AND d.project_id=m.project_id AND d.monitor_id=m.id
 RETURNING m.*
), output AS (SELECT * FROM inserted UNION ALL SELECT * FROM changed)
 SELECT d.result,CASE WHEN m.id IS NOT NULL THEN ` + publicJSON + ` END FROM decision d LEFT JOIN output m ON true`
