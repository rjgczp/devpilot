//go:build integration

package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func TestCleanupBatchMultiInstanceAndFaults(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	ctx := context.Background()
	users := user.Store{Pool: db}
	u, err := users.Create(ctx, "cleanup@example.com", password.DummyPHC)
	if err != nil {
		t.Fatal("cleanup user fixture")
	}
	// Synthetic digests are fixture data, not the production token algorithm.
	if _, err := db.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,created_at,expires_at)
		SELECT $1,decode(md5(n::text)||md5('fixture'||n::text),'hex'),statement_timestamp()-interval '2h',statement_timestamp()-interval '1h' FROM generate_series(1,2000) n`, u.ID); err != nil {
		t.Fatal("expired batch fixture")
	}
	if _, err := db.Exec(ctx, `INSERT INTO sessions(user_id,token_hash,created_at,expires_at,revoked_at)
		VALUES($1,decode(repeat('ee',32),'hex'),statement_timestamp()-interval '1h',statement_timestamp()+interval '1h',statement_timestamp())`, u.ID); err != nil {
		t.Fatal("revoked fixture")
	}
	snapshot, err := users.FindCredentials(ctx, u.Email)
	if err != nil {
		t.Fatal("credential fixture")
	}
	_, active, err := session.NewTokens().New()
	if err != nil {
		t.Fatal("active fixture")
	}
	store := session.Store{Pool: db}
	if _, err := store.Create(ctx, snapshot, active, 24*time.Hour); err != nil {
		t.Fatal("active fixture")
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal("row lock fixture")
	}
	defer tx.Rollback(ctx)
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM sessions WHERE expires_at<=statement_timestamp() ORDER BY expires_at,id LIMIT 1 FOR UPDATE`).Scan(&locked); err != nil {
		t.Fatal("candidate lock")
	}
	start := make(chan struct{})
	results := make(chan int64, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), session.CleanupTimeout)
			defer cancel()
			n, err := store.Cleanup(ctx)
			results <- n
			failures <- err
		}()
	}
	close(start)
	a, b := <-results, <-results
	if <-failures != nil || <-failures != nil || a != 1000 || b != 1000 {
		t.Fatal("multi-instance batch bound or locked-candidate wait")
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions`) != 2 || queryInt(t, db, `SELECT count(*) FROM sessions WHERE id=$1`, locked) != 1 {
		t.Fatal("overlapping deletes or skipped row lost")
	}
	if _, err := store.Lookup(ctx, active); err != nil {
		t.Fatal("cleanup deleted valid session")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal("unlock fixture")
	}
	if n, err := store.Cleanup(ctx); err != nil || n != 1 {
		t.Fatal("remaining candidate not cleaned")
	}
	if n, err := store.Cleanup(ctx); err != nil || n != 0 {
		t.Fatal("noneligible row removed")
	}
	lock, err := db.Begin(ctx)
	if err != nil {
		t.Fatal("timeout transaction")
	}
	if _, err := lock.Exec(ctx, `LOCK TABLE sessions IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal("timeout lock")
	}
	timeout, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	_, err = store.Cleanup(timeout)
	cancel()
	lock.Rollback(ctx)
	if err != user.ErrTimeout {
		t.Fatal("cleanup DB timeout classification")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.Cleanup(canceled); err != user.ErrCanceled {
		t.Fatal("cleanup cancellation propagation")
	}
	if _, err := db.Exec(ctx, `ALTER TABLE sessions RENAME TO sessions_hidden`); err != nil {
		t.Fatal("missing schema fixture")
	}
	if _, err := store.Cleanup(ctx); err != user.ErrSchema {
		t.Fatal("cleanup schema classification")
	}
	if _, err := db.Exec(ctx, `ALTER TABLE sessions_hidden RENAME TO sessions`); err != nil {
		t.Fatal("restore cleanup schema")
	}
}
