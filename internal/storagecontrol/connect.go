package storagecontrol

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OpenControl accepts only the two fixed control identities. It cannot open an
// API/Worker/Supervisor/administrator session or provision a role.
func OpenControl(ctx context.Context, cfg *pgxpool.Config, role string) (*pgxpool.Pool, error) {
	if role != "devpilot_storage_reporter" && role != "devpilot_scheduler_controller" {
		return nil, ErrReport
	}
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		return nil, ErrReport
	}
	var safe bool
	e = db.QueryRow(ctx, `SELECT session_user=current_user AND current_setting('server_version_num')::int/10000=18
 AND current_setting('statement_timeout')='2s' AND current_setting('transaction_timeout')='2s' AND current_setting('lock_timeout')='250ms'
 AND pg_has_role(session_user,$1,'MEMBER')
 AND NOT(SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=session_user)
 AND NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname<>session_user AND rolname<>$1 AND pg_has_role(session_user,oid,'MEMBER'))
 AND NOT has_database_privilege(session_user,current_database(),'CREATE') AND NOT has_database_privilege(session_user,current_database(),'TEMP')
 AND NOT has_schema_privilege(session_user,'public','CREATE') AND NOT has_schema_privilege(session_user,'scheduler_private','CREATE')
 AND NOT has_column_privilege(session_user,'public.users','password_hash','SELECT')
 AND NOT has_any_column_privilege(session_user,'public.scheduler_control','UPDATE')
 AND NOT has_any_column_privilege(session_user,'public.monitor_runs','UPDATE')
 AND NOT has_any_column_privilege(session_user,'public.monitor_runs','SELECT')
 AND NOT has_table_privilege(session_user,'public.http_monitors','SELECT')
 AND NOT has_table_privilege(session_user,'public.sessions','SELECT')`, role).Scan(&safe)
	if e != nil || !safe {
		db.Close()
		return nil, ErrReport
	}
	return db, nil
}
