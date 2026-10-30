//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rjgczp/devpilot/internal/monitor"
)

func TestSchedulerRuntimeRolesHaveOnlyNarrowCapabilities(t *testing.T) {
	db := schedulerHistoricalDB(t)
	ctx := context.Background()
	f, m := monitorFixture(t, db)
	// These static LOGIN fixtures exist only in the already-guarded disposable
	// cluster. They never inherit the non-LOGIN function owners.
	if _, err := db.Exec(ctx, `CREATE ROLE scheduler_test_worker LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_worker; CREATE ROLE scheduler_test_api LOGIN PASSWORD 'disposable_fixture_only' IN ROLE devpilot_scheduler_api`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(ctx, `DROP OWNED BY scheduler_test_worker,scheduler_test_api;DROP ROLE scheduler_test_worker,scheduler_test_api`); err != nil {
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
	worker := connect("scheduler_test_worker")
	api := connect("scheduler_test_api")
	for _, sql := range []string{
		`SELECT password_hash FROM public.users`, `SELECT token_hash FROM public.sessions`, `SELECT id FROM public.users`,
		`UPDATE public.http_monitors SET name='forged'`, `UPDATE public.monitor_activation_states SET desired_state='enabled'`,
		`UPDATE public.monitor_schedules SET expires_at=clock_timestamp()+interval '1 day'`, `UPDATE public.scheduler_control SET gate='fixture'`,
		`CREATE TABLE public.scheduler_forged(id integer)`, `CREATE TEMP TABLE scheduler_forged(id integer)`,
		`SET ROLE devpilot_scheduler_owner`, `SET ROLE devpilot_scheduler_api_owner`, `CREATE ROLE scheduler_forged`,
		`SELECT * FROM scheduler_private.authorize(NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,60)`,
	} {
		if _, err := worker.Exec(ctx, sql); err == nil {
			t.Fatal("worker accepted forbidden capability", sql)
		}
	}
	var elevated bool
	if err := worker.QueryRow(ctx, `SELECT rolsuper OR rolbypassrls OR rolcreaterole OR rolreplication OR rolcreatedb FROM pg_roles WHERE rolname=session_user`).Scan(&elevated); err != nil || elevated {
		t.Fatal("elevated worker identity", err)
	}
	var n int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='scheduler_private' AND p.prosecdef AND (p.proconfig IS NULL OR NOT 'search_path=pg_catalog, scheduler_private'=ANY(p.proconfig) OR EXISTS(SELECT 1 FROM aclexplode(COALESCE(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'))`).Scan(&n); err != nil || n != 0 {
		t.Fatal("unsafe definer configuration", err, n)
	}
	// A real restricted API LOGIN can invoke only the accepted authenticated API
	// functions; the worker cannot borrow their token-hash access through EXECUTE.
	s := monitor.Store{Pool: api}
	a := activationRead(t, db, f.od, m)
	auth, err := s.ExecutionAuthorize(ctx, f.od, m.WorkspaceID, m.ProjectID, m.ID, monitor.ExecutionAuthorizationInput{ConfigurationVersion: "1", ConsentProfile: "periodic-http-get-v1", AuthorizationSeconds: 60}, monitor.ActivationETag(a))
	if err != nil || auth.ActivationGeneration != "2" {
		t.Fatal("narrow API grant failed", err)
	}
	status, err := s.ExecutionStatus(ctx, f.md, m.WorkspaceID, m.ProjectID, m.ID)
	if err != nil || status.AuthorizationState != "valid" || status.SchedulingState != "blocked" {
		t.Fatal("narrow status failed", err, status)
	}
	for _, sql := range []string{`SELECT * FROM public.monitor_runs`, `UPDATE public.monitor_schedules SET expires_at=clock_timestamp()+interval '1 day'`, `SELECT token_hash FROM public.sessions`} {
		if _, err := api.Exec(ctx, sql); err == nil {
			t.Fatal("API grant escaped its function", sql)
		}
	}
}
