-- Executable Checkpoint1 maintenance-protocol reference, GUARDED TEST DB ONLY.
-- Not a production rollback tool or a physical Supervisor attestation.
BEGIN;
SET LOCAL statement_timeout='2s';
SET LOCAL lock_timeout='250ms';
LOCK TABLE public.http_monitors IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE public.monitor_activation_states IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF current_database()<>'devpilot_registration_test'
 OR current_setting('server_version_num')::integer NOT BETWEEN 180000 AND 189999 THEN
  RAISE EXCEPTION 'guarded PG18 fixture required';
 END IF;
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE gate<>'closed' OR (kind='http' AND routable))
 OR EXISTS(SELECT 1 FROM public.monitor_schedules WHERE authorized_configuration_version IS NOT NULL)
 OR EXISTS(SELECT 1 FROM public.monitor_runs WHERE physical_release_confirmed_at IS NULL) THEN
  RAISE EXCEPTION 'projection_retirement_requires_drain_revoke_stop';
 END IF;
END $$;
UPDATE public.monitor_activation_states SET desired_state='disabled',activation_configuration_version=NULL,
 activation_generation=activation_generation+1,last_transition_kind='configuration_changed',last_actor_user_id=NULL,
 updated_at=GREATEST(updated_at,clock_timestamp());
COMMIT;
