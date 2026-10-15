//go:build integration

package integration

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/password"
	"github.com/rjgczp/devpilot/internal/session"
	"github.com/rjgczp/devpilot/internal/user"
	"github.com/rjgczp/devpilot/internal/workspace"
)

func workspaceDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	db := openDB(t)
	migrate(t)
	t.Cleanup(func() { goose(t, filepath.Join(root(), "migrations"), "down-to 0", true) })
	return db
}

func workspaceIdentity(t *testing.T, db *pgxpool.Pool, email string) (user.Public, string, session.Digest) {
	t.Helper()
	u, err := (user.Store{Pool: db}).Create(context.Background(), email, password.DummyPHC)
	if err != nil {
		t.Fatal("workspace account fixture")
	}
	secret, digest, err := session.NewTokens().New()
	if err != nil {
		t.Fatal("workspace token fixture")
	}
	_, err = (session.Store{Pool: db}).Create(context.Background(), user.Credentials{Public: u, PasswordHash: password.DummyPHC}, digest, time.Hour)
	if err != nil {
		t.Fatal("workspace session fixture")
	}
	return u, secret, digest
}

func TestWorkspaceMigrationAndConstraints(t *testing.T) {
	db := openDB(t)
	dir := filepath.Join(root(), "migrations")
	goose(t, dir, "down-to 0", true)
	t.Cleanup(func() { goose(t, dir, "down-to 0", true) })
	goose(t, dir, "up-to 2", true)
	u, _, digest := workspaceIdentity(t, db, "upgrade-workspace@example.com")
	goose(t, dir, "up", true)
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`) != 3 || queryInt(t, db, `SELECT count(*) FROM sessions WHERE token_hash=$1`, digest[:]) != 1 {
		t.Fatal("version-2 upgrade/repeat changed existing session")
	}
	store := workspace.Store{Pool: db}
	w, err := store.Create(context.Background(), digest, "研发团队")
	if err != nil || w.ID[14] != '7' || w.Role != "owner" || w.CreatedAt != w.UpdatedAt || w.CreatedAt.Location() != time.UTC {
		t.Fatal("workspace UUID/defaults/projection")
	}
	for _, name := range []string{"", " ", " leading", "trailing ", "a\nb", "a\x01b", "a\x1fb", "a\x7fb", strings.Repeat("a", 101), strings.Repeat("🔐", 101)} {
		if _, err := db.Exec(context.Background(), `INSERT INTO workspaces(name) VALUES($1)`, name); err == nil {
			t.Fatal("invalid direct workspace name accepted")
		}
	}
	for _, tc := range []struct {
		sql  string
		args []any
		code string
	}{
		{`INSERT INTO workspaces(id,name) VALUES(NULL,'x')`, nil, "23502"},
		{`INSERT INTO workspaces(name) VALUES(NULL)`, nil, "23502"},
		{`INSERT INTO workspaces(name,created_at) VALUES('x',NULL)`, nil, "23502"},
		{`INSERT INTO workspaces(name,updated_at) VALUES('x',NULL)`, nil, "23502"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(NULL,$1,'member')`, []any{u.ID}, "23502"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,NULL,'member')`, []any{w.ID}, "23502"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,NULL)`, []any{w.ID, u.ID}, "23502"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'admin')`, []any{w.ID, u.ID}, "23514"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES(uuidv7(),$1,'member')`, []any{u.ID}, "23503"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,uuidv7(),'member')`, []any{w.ID}, "23503"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, []any{w.ID, u.ID}, "23505"},
		{`INSERT INTO workspace_members(workspace_id,user_id,role,created_at) VALUES($1,$2,'member',NULL)`, []any{w.ID, u.ID}, "23502"},
	} {
		_, err := db.Exec(context.Background(), tc.sql, tc.args...)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != tc.code {
			t.Fatal("expected database constraint category was not enforced")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_indexes WHERE tablename='workspace_members' AND indexname IN ('workspace_members_pkey','workspace_members_user_workspace_idx')`) != 2 {
		t.Fatal("membership indexes")
	}
	if _, err := db.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, u.ID); err == nil || queryInt(t, db, `SELECT count(*) FROM sessions WHERE user_id=$1`, u.ID) != 1 {
		t.Fatal("user deletion must be restricted atomically")
	}
	if _, err := db.Exec(context.Background(), `DELETE FROM workspaces WHERE id=$1`, w.ID); err != nil || queryInt(t, db, `SELECT count(*) FROM workspace_members`) != 0 {
		t.Fatal("workspace membership cascade")
	}
	goose(t, dir, "down", true)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('workspaces','workspace_members')`) != 0 || queryInt(t, db, `SELECT count(*) FROM users WHERE id=$1`, u.ID) != 1 || queryInt(t, db, `SELECT count(*) FROM sessions WHERE user_id=$1`, u.ID) != 1 {
		t.Fatal("version-3 down damaged previous data")
	}
	goose(t, dir, "up", true)
	goose(t, dir, "down-to 0", true)
	goose(t, dir, "up", true)
	if queryInt(t, db, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('users','sessions','workspaces','workspace_members')`) != 4 {
		t.Fatal("fresh migration chain")
	}
}

func TestWorkspaceTenantQueriesAndConcurrentCreation(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	_, _, a := workspaceIdentity(t, db, "tenant-a@example.com")
	ub, _, b := workspaceIdentity(t, db, "tenant-b@example.com")
	_, _, c := workspaceIdentity(t, db, "tenant-c@example.com")
	store := workspace.Store{Pool: db}
	x, err := store.Create(ctx, a, "X")
	if err != nil {
		t.Fatal("X creation")
	}
	y, err := store.Create(ctx, b, "Y")
	if err != nil {
		t.Fatal("Y creation")
	}
	z, err := store.Create(ctx, b, "Z")
	if err != nil {
		t.Fatal("Z creation")
	}
	_, err = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) SELECT $1,user_id,'member' FROM sessions WHERE token_hash=$2`, y.ID, a[:])
	if err != nil {
		t.Fatal("A member Y fixture")
	}
	if _, err = db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, x.ID, ub.ID); err != nil {
		t.Fatal("B member X fixture")
	}
	if got, err := store.Detail(ctx, a, y.ID); err != nil || got.Role != "member" {
		t.Fatal("workspace-local role")
	}
	for _, id := range []string{z.ID, "00000000-0000-0000-0000-000000000000", "bad", strings.ToUpper(x.ID)} {
		if _, err := store.Detail(ctx, a, id); !errors.Is(err, workspace.ErrNotFound) {
			t.Fatal("tenant/malformed detail not hidden")
		}
	}
	if _, err := store.Detail(ctx, c, x.ID); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatal("outsider")
	}
	empty, err := store.List(ctx, c, 50, "")
	if err != nil || empty.Data == nil || len(empty.Data) != 0 || empty.Pagination.NextAfter != nil {
		t.Fatal("valid identity empty list")
	}
	if _, err := store.List(ctx, session.Digest{}, 50, ""); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("invalid identity is not empty list")
	}
	var ids []string
	after := ""
	for {
		page, err := store.List(ctx, a, 1, after)
		if err != nil || len(page.Data) != 1 {
			t.Fatal("bounded page")
		}
		ids = append(ids, page.Data[0].ID)
		if page.Pagination.NextAfter == nil {
			break
		}
		after = *page.Pagination.NextAfter
	}
	if len(ids) != 2 || ids[0] >= ids[1] {
		t.Fatal("stable pagination lost/duplicated membership")
	}
	for _, cursor := range []string{z.ID, "00000000-0000-0000-0000-000000000000"} {
		page, err := store.List(ctx, a, 100, cursor)
		if err != nil {
			t.Fatal("cursor must not require membership/existence")
		}
		for _, row := range page.Data {
			if row.ID == z.ID || row.ID <= cursor {
				t.Fatal("cursor tenant leak")
			}
		}
	}
	const n = 6
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := store.Create(ctx, a, "same name"); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent same-name create")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM workspaces w JOIN workspace_members m ON m.workspace_id=w.id WHERE w.name='same name' AND m.role='owner'`) != n {
		t.Fatal("concurrent owner atomicity")
	}
	// Real unique-key race: B is not yet a member of a newly created workspace.
	other, err := store.Create(ctx, a, "pair race")
	if err != nil {
		t.Fatal("pair fixture")
	}
	errs = make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := db.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'member')`, other.ID, ub.ID)
			errs <- err
		}()
	}
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-errs
		var pg *pgconn.PgError
		if err == nil {
			wins++
		} else if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workspace_members_pkey" {
			conflicts++
		} else {
			t.Fatal("unexpected pair race category")
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("duplicate membership race")
	}
}
