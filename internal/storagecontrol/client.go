package storagecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// Publisher holds only the independently approved Reporter LOGIN. Runtime
// Worker/Controller/HTTP identities cannot publish or invalidate samples.
type Publisher interface {
	Begin(context.Context) (Challenge, error)
	Publish(context.Context, Challenge, any) error
	Unavailable(context.Context) error
}
type Challenge struct {
	Nonce    string          `json:"nonce"`
	Sequence int64           `json:"sequence"`
	Issued   time.Time       `json:"issued_at"`
	Deadline time.Time       `json:"deadline"`
	Manifest json.RawMessage `json:"manifest"`
}
type Database interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}
type Client struct {
	DB          Database
	Incarnation string
}

var ErrReport = errors.New("storage publication unavailable")

func (c Client) Begin(ctx context.Context) (Challenge, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var raw []byte
	var out Challenge
	if c.DB.QueryRow(ctx, `SELECT scheduler_private.storage_begin($1::uuid)`, c.Incarnation).Scan(&raw) != nil || json.Unmarshal(raw, &out) != nil || out.Nonce == "" || out.Sequence < 1 || !out.Deadline.After(out.Issued) {
		return Challenge{}, ErrReport
	}
	return out, nil
}
func (c Client) Publish(ctx context.Context, ch Challenge, metrics any) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	raw, e := json.Marshal(metrics)
	if e != nil || len(raw) > 4096 {
		return ErrReport
	}
	var result []byte
	if c.DB.QueryRow(ctx, `SELECT scheduler_private.storage_report($1::uuid,$2::uuid,$3::jsonb)`, c.Incarnation, ch.Nonce, raw).Scan(&result) != nil {
		return ErrReport
	}
	var ack struct {
		Accepted bool `json:"accepted"`
	}
	if json.Unmarshal(result, &ack) != nil || !ack.Accepted {
		return ErrReport
	}
	return nil
}
func (c Client) Unavailable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var result []byte
	if c.DB.QueryRow(ctx, `SELECT scheduler_private.storage_report($1::uuid,NULL::uuid,NULL::jsonb)`, c.Incarnation).Scan(&result) != nil {
		return ErrReport
	}
	return nil
}
