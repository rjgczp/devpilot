-- Product-approved exact diff (2026-11-11): template and disposable PG18 only.
-- Explicit offline provision command; never an application startup hook.
-- Scope: a NEW dedicated devpilot_private database after existing M1-10,12,13.
-- Applied only by the offline migrator, never the application. No scheduler
-- owner/worker/controller/supervisor membership or new function EXECUTE grant.
CREATE ROLE devpilot_private_app LOGIN INHERIT NOSUPERUSER NOCREATEDB
  NOCREATEROLE NOREPLICATION NOBYPASSRLS;
-- Password comes from a private file through psql's quoted variable, not source.
ALTER ROLE devpilot_private_app PASSWORD :'app_password';
REVOKE ALL ON DATABASE devpilot_private FROM PUBLIC;
GRANT CONNECT ON DATABASE devpilot_private TO devpilot_private_app;
GRANT USAGE ON SCHEMA public TO devpilot_private_app;
GRANT devpilot_scheduler_api TO devpilot_private_app;

GRANT SELECT(id,email,password_hash,status,created_at), INSERT(email,password_hash),
  UPDATE(id) ON public.users TO devpilot_private_app;
GRANT SELECT ON public.sessions TO devpilot_private_app;
GRANT INSERT(user_id,token_hash,expires_at), UPDATE(revoked_at), DELETE
  ON public.sessions TO devpilot_private_app;
GRANT SELECT, INSERT(name), UPDATE(id)
  ON public.workspaces TO devpilot_private_app;
GRANT SELECT, INSERT(workspace_id,user_id,role,created_at), UPDATE(role), DELETE
  ON public.workspace_members TO devpilot_private_app;
GRANT SELECT,
  INSERT(workspace_id,invitee_user_id,created_by_user_id,created_at,updated_at,expires_at),
  UPDATE(status,updated_at) ON public.workspace_invitations TO devpilot_private_app;
GRANT SELECT, INSERT(workspace_id,name,description,created_by_user_id),
  UPDATE(name,description,version,updated_at)
  ON public.projects TO devpilot_private_app;
GRANT SELECT,
  INSERT(workspace_id,project_id,name,url,method,interval_seconds,timeout_seconds,expected_status,created_by_user_id),
  UPDATE(name,url,method,interval_seconds,timeout_seconds,expected_status,version,updated_at)
  ON public.http_monitors TO devpilot_private_app;
-- The unchanged invoker initialization/invalidation triggers require these
-- Activation privileges; UPDATE(id/role) above is also required for row locks.
GRANT SELECT, INSERT(workspace_id,project_id,monitor_id),
  UPDATE(desired_state,activation_configuration_version,activation_generation,
    next_enable_allowed_at,last_transition_kind,last_actor_user_id,updated_at)
  ON public.monitor_activation_states TO devpilot_private_app;
-- No DDL, TEMP, TRUNCATE, goose table, direct scheduler table DML, or ownership.
-- Existing devpilot_scheduler_api provides only authorize/status/history calls.
