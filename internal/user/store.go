package user

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConflict    = errors.New("email conflict")
	ErrUnavailable = errors.New("database unavailable")
	ErrTimeout     = errors.New("database timeout")
	ErrCanceled    = errors.New("request canceled")
	ErrSchema      = errors.New("schema missing")
	ErrInternal    = errors.New("user persistence error")
	ErrNotFound    = errors.New("user not found")
)

// Public intentionally has no password hash, verification claim or internal fields.
type Public struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct{ Pool *pgxpool.Pool }

// Credentials is internal only. HTTP responses always project Public instead.
type Credentials struct {
	Public
	PasswordHash string
}

func (s Store) FindCredentials(ctx context.Context, email string) (Credentials, error) {
	var u Credentials
	err := s.Pool.QueryRow(ctx, `SELECT id::text,email,status,created_at,password_hash FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.Status, &u.CreatedAt, &u.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Credentials{}, ErrNotFound
	}
	if err != nil {
		return Credentials{}, Classify(ctx, err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

func (s Store) Create(ctx context.Context, email, hash string) (Public, error) {
	var u Public
	err := s.Pool.QueryRow(ctx, `INSERT INTO users (email, password_hash) VALUES ($1, $2)
		RETURNING id::text, email, status, created_at`, email, hash).Scan(&u.ID, &u.Email, &u.Status, &u.CreatedAt)
	if err != nil {
		return Public{}, Classify(ctx, err)
	}
	u.CreatedAt = u.CreatedAt.UTC()
	return u, nil
}

// Classify returns only safe categories; no driver error escapes this boundary.
// An error cannot establish whether a submitted INSERT committed. Never retry here.
func Classify(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return ErrCanceled
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || pgconn.Timeout(err) {
		return ErrTimeout
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "23505" && pg.ConstraintName == "users_email_key":
			return ErrConflict
		case pg.Code == "42P01":
			return ErrSchema
		case pg.Code == "57014" && pg.Message == "canceling statement due to statement timeout":
			return ErrTimeout
		case len(pg.Code) >= 2 && pg.Code[:2] == "08", pg.Code == "57P01", pg.Code == "57P02", pg.Code == "57P03":
			return ErrUnavailable
		default:
			return ErrInternal
		}
	}
	var connection *pgconn.ConnectError
	var network net.Error
	if errors.As(err, &connection) || errors.As(err, &network) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) || pgconn.SafeToRetry(err) {
		return ErrUnavailable
	}
	return ErrInternal
}
