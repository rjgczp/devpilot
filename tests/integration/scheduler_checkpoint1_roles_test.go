//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCheckpointOneControllerPublicAndIncarnation(t *testing.T) {
	db := workspaceDB(t)
	ctx := context.Background()
	if _, err := db.Exec(ctx, `CREATE ROLE checkpoint1_controller LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_controller;CREATE ROLE checkpoint1_public LOGIN PASSWORD 'disposable_fixture_only'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP OWNED BY checkpoint1_controller,checkpoint1_public;DROP ROLE checkpoint1_controller,checkpoint1_public`); err != nil {
			t.Error(err)
		}
	})
	connect := func(name string) *pgxpool.Pool {
		cfg := db.Config()
		cfg.ConnConfig.User = name
		cfg.ConnConfig.Password = "disposable_fixture_only"
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(p.Close)
		return p
	}
	for _, p := range []*pgxpool.Pool{connect("checkpoint1_controller"), connect("checkpoint1_public")} {
		for _, sql := range []string{`SELECT password_hash FROM users`, `SELECT token_hash FROM sessions`, `SELECT * FROM monitor_schedules`, `UPDATE monitor_runs SET physical_release_confirmed_at=clock_timestamp()`, `CREATE TABLE public.checkpoint1_forged(i int)`, `CREATE TEMP TABLE checkpoint1_forged(i int)`, `SET ROLE devpilot_scheduler_owner`, `SET ROLE devpilot_scheduler_api_owner`, `SELECT scheduler_private.authorize(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,60)`, `SELECT scheduler_private.consume_input(NULL,NULL,NULL,NULL,NULL,NULL,1)`} {
			_, err := p.Exec(ctx, sql)
			checkpointReject(t, err, "42501")
		}
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_roles WHERE rolname IN ('devpilot_scheduler_owner','devpilot_scheduler_api_owner') AND (rolcanlogin OR rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication)`) != 0 {
		t.Fatal("elevated function owner")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_auth_members m JOIN pg_roles r ON r.oid=m.roleid WHERE r.rolname IN ('devpilot_scheduler_owner','devpilot_scheduler_api_owner')`) != 0 {
		t.Fatal("runtime membership of function owner")
	}
	if queryInt(t, db, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='scheduler_private' AND (EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE') OR (p.prosecdef AND (p.proconfig IS NULL OR NOT 'search_path=pg_catalog, scheduler_private'=ANY(p.proconfig) OR p.prosrc ~* '\mEXECUTE\M')))`) != 0 {
		t.Fatal("PUBLIC execution, unsafe search_path or dynamic SQL")
	}
	for _, pair := range [][2]string{{"users", "password_hash"}, {"sessions", "token_hash"}} {
		var allowed bool
		if err := db.QueryRow(ctx, `SELECT has_column_privilege('devpilot_scheduler_owner',$1,$2,'SELECT')`, pair[0], pair[1]).Scan(&allowed); err != nil || allowed {
			t.Fatal("system definer reads user secrets", err)
		}
	}
	worker, inc := schedulerWorkerLogin(t, db, "scheduler_claim_worker1")
	var hints []byte
	if err := worker.QueryRow(ctx, `SELECT scheduler_private.candidates($1::uuid,NULL)`, inc).Scan(&hints); err != nil {
		t.Fatal("registered bounded identity denied", err)
	}
	var forged string
	if err := db.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&forged); err != nil {
		t.Fatal(err)
	}
	_, err := worker.Exec(ctx, `SELECT scheduler_private.candidates($1::uuid,NULL)`, forged)
	checkpointReject(t, err, "42501")
	if _, err = db.Exec(ctx, `UPDATE scheduler_control SET incarnation=$1::uuid WHERE login_name='scheduler_claim_worker1'`, forged); err != nil {
		t.Fatal(err)
	}
	_, err = worker.Exec(ctx, `SELECT scheduler_private.candidates($1::uuid,NULL)`, inc)
	checkpointReject(t, err, "42501")
	// A caller cannot obtain elevated identity by SET ROLE or a copied parameter.
	_, err = worker.Exec(ctx, `SET ROLE devpilot_scheduler_owner`)
	checkpointReject(t, err, "42501")
}
