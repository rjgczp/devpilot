package session

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/user"
)

var ErrInvalid = errors.New("invalid session or credential snapshot")

type Created struct {
	User      user.Public
	CreatedAt time.Time
	ExpiresAt time.Time
}

type Store struct{ Pool *pgxpool.Pool }

// Create rechecks the verified snapshot under FOR SHARE: FOR KEY SHARE would
// permit concurrent updates to status/password_hash. No lock spans Argon work.
func (s Store) Create(ctx context.Context, snapshot user.Credentials, digest Digest, ttl time.Duration) (Created, error) {
	if ttl < 5*time.Minute || ttl > 24*time.Hour {
		return Created{}, user.ErrInternal
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Created{}, user.Classify(ctx, err)
	}
	defer tx.Rollback(ctx)
	var current user.Credentials
	err = tx.QueryRow(ctx, `SELECT id::text,email,status,created_at,password_hash FROM users WHERE id=$1 FOR SHARE`, snapshot.ID).Scan(&current.ID, &current.Email, &current.Status, &current.CreatedAt, &current.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Created{}, ErrInvalid
	}
	if err != nil {
		return Created{}, user.Classify(ctx, err)
	}
	if current.Status != "active" || current.PasswordHash != snapshot.PasswordHash {
		return Created{}, ErrInvalid
	}
	if ctx.Err() != nil {
		return Created{}, user.Classify(ctx, ctx.Err())
	}
	result := Created{User: current.Public}
	err = tx.QueryRow(ctx, `INSERT INTO sessions(user_id,token_hash,expires_at)
		VALUES($1,$2,statement_timestamp()+($3::bigint * interval '1 microsecond'))
		RETURNING created_at,expires_at`, current.ID, digest[:], ttl.Microseconds()).Scan(&result.CreatedAt, &result.ExpiresAt)
	if err != nil {
		return Created{}, user.Classify(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Created{}, user.Classify(ctx, err)
	}
	result.User.CreatedAt = result.User.CreatedAt.UTC()
	result.CreatedAt = result.CreatedAt.UTC()
	result.ExpiresAt = result.ExpiresAt.UTC()
	return result, nil
}

func (s Store) Lookup(ctx context.Context, digest Digest) (user.Public, error) {
	var u user.Public
	err := s.Pool.QueryRow(ctx, `SELECT u.id::text,u.email,u.status,u.created_at
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at>statement_timestamp()
		AND s.revoked_at IS NULL AND u.status='active'`, digest[:]).Scan(&u.ID, &u.Email, &u.Status, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return user.Public{}, ErrInvalid
	}
	if err != nil {
		return user.Public{}, user.Classify(ctx, err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

func (s Store) Revoke(ctx context.Context, digest Digest) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at=COALESCE(revoked_at,statement_timestamp()) WHERE token_hash=$1`, digest[:])
	if err != nil {
		return user.Classify(ctx, err)
	}
	return nil
}

func (s Store) Cleanup(ctx context.Context) (int64, error) {
	result, err := s.Pool.Exec(ctx, `WITH candidates AS (
		SELECT id FROM sessions WHERE expires_at<=statement_timestamp()
		OR revoked_at<=statement_timestamp()
		ORDER BY expires_at,id LIMIT 1000 FOR UPDATE SKIP LOCKED
	) DELETE FROM sessions s USING candidates c WHERE s.id=c.id`)
	if err != nil {
		return 0, user.Classify(ctx, err)
	}
	return result.RowsAffected(), nil
}
