//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
)

func TestSessionStoreAndUpgrade(t *testing.T) {
	db := openDB(t)
	goose(t, filepath.Join(root(), "migrations"), "down-to 0", true)
	goose(t, filepath.Join(root(), "migrations"), "up-to 1", true)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	ctx := context.Background()
	hash, err := password.New().Hash(testPassword)
	if err != nil {
		t.Fatal("hash fixture")
	}
	users := user.Store{Pool: db}
	u, err := users.Create(ctx, "session@example.com", hash)
	if err != nil {
		t.Fatal("old-schema user")
	}
	migrate(t)
	migrate(t)
	credential, err := users.FindCredentials(ctx, u.Email)
	if err != nil || credential.PasswordHash != hash || credential.ID != u.ID {
		t.Fatal("upgrade changed credentials")
	}
	store := session.Store{Pool: db}
	secret, digest, err := session.NewTokens().New()
	if err != nil {
		t.Fatal("token fixture")
	}
	created, err := store.Create(ctx, credential, digest, 24*time.Hour)
	if err != nil || created.User.ID != u.ID || created.ExpiresAt.Sub(created.CreatedAt) != 24*time.Hour {
		t.Fatal("session creation or default lifetime")
	}
	var stored []byte
	if db.QueryRow(ctx, `SELECT token_hash FROM sessions WHERE user_id=$1`, u.ID).Scan(&stored) != nil || string(stored) != string(digest[:]) || string(stored) == secret {
		t.Fatal("digest-only storage")
	}
	for i := 0; i < 2; i++ {
		if got, err := store.Lookup(ctx, digest); err != nil || got.ID != u.ID {
			t.Fatal("live identity")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions WHERE created_at=$1 AND expires_at=$2`, created.CreatedAt, created.ExpiresAt) != 1 {
		t.Fatal("lookup renewed session")
	}
	_, second, err := session.NewTokens().New()
	if err != nil {
		t.Fatal("token fixture")
	}
	if _, err := store.Create(ctx, credential, second, 24*time.Hour); err != nil {
		t.Fatal("independent session")
	}
	if _, err := store.Create(ctx, credential, digest, 24*time.Hour); !errors.Is(err, user.ErrInternal) {
		t.Fatal("digest conflict must not retry or be email conflict")
	}
	for _, sql := range []string{
		`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES(NULL,decode(repeat('01',32),'hex'),statement_timestamp()+interval '1h')`,
		`INSERT INTO sessions(user_id,token_hash,expires_at) VALUES(uuidv7(),decode(repeat('01',32),'hex'),statement_timestamp()+interval '1h')`,
		`INSERT INTO sessions(user_id,token_hash,expires_at) SELECT id,decode('01','hex'),statement_timestamp()+interval '1h' FROM users LIMIT 1`,
		`INSERT INTO sessions(user_id,token_hash,expires_at) SELECT id,NULL,statement_timestamp()+interval '1h' FROM users LIMIT 1`,
		`INSERT INTO sessions(user_id,token_hash,expires_at) SELECT id,decode(repeat('01',32),'hex'),statement_timestamp() FROM users LIMIT 1`,
		`INSERT INTO sessions(user_id,token_hash,expires_at,revoked_at) SELECT id,decode(repeat('01',32),'hex'),statement_timestamp()+interval '1h',statement_timestamp()-interval '1s' FROM users LIMIT 1`,
	} {
		if _, err := db.Exec(ctx, sql); err == nil {
			t.Fatal("session constraint bypass")
		}
	}
	if _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, u.ID); err != nil {
		t.Fatal("disable fixture")
	}
	if _, err := store.Lookup(ctx, digest); err != session.ErrInvalid {
		t.Fatal("disabled session authenticated")
	}
	if _, err := store.Create(ctx, credential, session.Digest{1}, 24*time.Hour); err != session.ErrInvalid {
		t.Fatal("disabled snapshot created")
	}
	if _, err := db.Exec(ctx, `UPDATE users SET status='active',password_hash=$2 WHERE id=$1`, u.ID, password.DummyPHC); err != nil {
		t.Fatal("changed credential fixture")
	}
	if _, err := store.Create(ctx, credential, session.Digest{2}, 24*time.Hour); err != session.ErrInvalid {
		t.Fatal("stale password snapshot created")
	}
	if _, err := db.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, hash); err != nil {
		t.Fatal("restore fixture")
	}
	// No cleanup worker is running. Rows remain while expiry and revoke deny.
	if _, err := db.Exec(ctx, `UPDATE sessions SET created_at=statement_timestamp()-interval '2h',expires_at=statement_timestamp()-interval '1h' WHERE token_hash=$1`, digest[:]); err != nil {
		t.Fatal("expiry fixture")
	}
	if _, err := store.Lookup(ctx, digest); err != session.ErrInvalid || queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, digest[:]) != 1 {
		t.Fatal("expiry depends on deletion")
	}
	if err := store.Revoke(ctx, second); err != nil {
		t.Fatal("revoke")
	}
	if err := store.Revoke(ctx, second); err != nil {
		t.Fatal("idempotent revoke")
	}
	if _, err := store.Lookup(ctx, second); err != session.ErrInvalid || queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, second[:]) != 1 {
		t.Fatal("revoke depends on deletion")
	}
	goose(t, filepath.Join(root(), "migrations"), "down-to 2", true)
	goose(t, filepath.Join(root(), "migrations"), "down", true)
	if queryInt(t, db, `SELECT count(*) FROM users WHERE id=$1`, u.ID) != 1 {
		t.Fatal("session down deleted user")
	}
	migrate(t)
	if _, err := store.Create(ctx, credential, digest, 24*time.Hour); err != nil {
		t.Fatal("session reapply")
	}
	if _, err := db.Exec(ctx, `DELETE FROM users WHERE id=$1`, u.ID); err != nil || queryInt(t, db, `SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("foreign key cascade")
	}
}

func TestSessionCreationRechecksConcurrentUpdate(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	users := user.Store{Pool: db}
	u, err := users.Create(ctx, "racing-session@example.com", password.DummyPHC)
	if err != nil {
		t.Fatal("user fixture")
	}
	snapshot, err := users.FindCredentials(ctx, u.Email)
	if err != nil {
		t.Fatal("credential fixture")
	}
	for _, update := range []string{`UPDATE users SET status='disabled' WHERE id=$1`, `UPDATE users SET password_hash='changed' WHERE id=$1`} {
		if _, err := db.Exec(ctx, `UPDATE users SET status='active',password_hash=$2 WHERE id=$1`, u.ID, password.DummyPHC); err != nil {
			t.Fatal("reset fixture")
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal("lock fixture")
		}
		if _, err = tx.Exec(ctx, update, u.ID); err != nil {
			t.Fatal("concurrent update")
		}
		result := make(chan error, 1)
		go func() {
			_, err := (session.Store{Pool: db}).Create(ctx, snapshot, session.Digest{3}, 24*time.Hour)
			result <- err
		}()
		for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM users WHERE id=$1 FOR SHARE%'`) == 0 {
			if ctx.Err() != nil {
				t.Fatal("creation did not wait for updated row")
			}
			time.Sleep(time.Millisecond)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal("update commit")
		}
		if err := <-result; err != session.ErrInvalid {
			t.Fatal("stale credential authenticated after wait")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM sessions`) != 0 {
		t.Fatal("rejected race inserted sessions")
	}
}

func TestCreationLockSerializesDisableAndConcurrentRevocation(t *testing.T) {
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	users := user.Store{Pool: db}
	u, err := users.Create(ctx, "share-lock@example.com", password.DummyPHC)
	if err != nil {
		t.Fatal("user fixture")
	}
	snapshot, err := users.FindCredentials(ctx, u.Email)
	if err != nil {
		t.Fatal("snapshot fixture")
	}
	lock, err := db.Begin(ctx)
	if err != nil {
		t.Fatal("insert gate")
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, `LOCK TABLE sessions IN SHARE MODE`); err != nil {
		t.Fatal("insert gate")
	}
	store := session.Store{Pool: db}
	_, digest, err := session.NewTokens().New()
	if err != nil {
		t.Fatal("token fixture")
	}
	created := make(chan error, 1)
	go func() { _, err := store.Create(ctx, snapshot, digest, 24*time.Hour); created <- err }()
	wait := func(query string) {
		for queryInt(t, db, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1`, query) == 0 {
			if ctx.Err() != nil {
				t.Fatal("expected database lock not observed")
			}
			time.Sleep(time.Millisecond)
		}
	}
	wait("INSERT INTO sessions%")
	updated := make(chan error, 1)
	go func() { _, err := db.Exec(ctx, `UPDATE users SET status='disabled' WHERE id=$1`, u.ID); updated <- err }()
	wait("UPDATE users SET status='disabled'%")
	if err := lock.Commit(ctx); err != nil {
		t.Fatal("release insert gate")
	}
	if <-created != nil || <-updated != nil {
		t.Fatal("login then disable ordering")
	}
	if _, err := store.Lookup(ctx, digest); err != session.ErrInvalid {
		t.Fatal("identity trusted creation state after disable")
	}
	if _, err := db.Exec(ctx, `UPDATE users SET status='active' WHERE id=$1`, u.ID); err != nil {
		t.Fatal("restore status")
	}
	prior, err := store.Lookup(ctx, digest)
	if err != nil {
		t.Fatal("pre-revoke identity")
	}
	start := make(chan struct{})
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; result <- store.Revoke(ctx, digest) }()
	}
	close(start)
	if <-result != nil || <-result != nil {
		t.Fatal("concurrent revoke not idempotent")
	}
	if prior.ID != u.ID {
		t.Fatal("prior snapshot changed")
	}
	if _, err := store.Lookup(ctx, digest); err != session.ErrInvalid {
		t.Fatal("post-commit revoke accepted")
	}
	// Structured SQL-looking bytes are data in a digest parameter, never SQL.
	var probe session.Digest
	copy(probe[:], []byte("'; DROP TABLE users; --"))
	if _, err := store.Create(ctx, snapshot, probe, 24*time.Hour); err != nil || queryInt(t, db, `SELECT count(*) FROM users WHERE id=$1`, u.ID) != 1 {
		t.Fatal("digest parameter changed statement")
	}
}
