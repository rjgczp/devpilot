-- +goose Up
-- Match the established Monitor -> Activation direction. All retirement, DDL,
-- privileges and the Goose version receipt commit in this one transaction.
LOCK TABLE public.http_monitors IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE public.monitor_activation_states IN ACCESS EXCLUSIVE MODE;
ALTER TABLE public.monitor_activation_states DROP CONSTRAINT monitor_activation_states_check1;
ALTER TABLE public.monitor_activation_states ADD CONSTRAINT monitor_activation_states_check1 CHECK (
 (desired_state='enabled' AND activation_configuration_version IS NOT NULL AND next_enable_allowed_at IS NOT NULL
  AND last_actor_user_id IS NOT NULL AND last_transition_kind='owner_enabled')
 OR (desired_state='disabled' AND activation_configuration_version IS NULL AND (
  (last_transition_kind='owner_paused' AND last_actor_user_id IS NOT NULL)
  OR (last_transition_kind IN ('never_activated','configuration_changed','upgrade_revoked') AND last_actor_user_id IS NULL)))
);
-- The original generation/cooling/identity guards remain enabled. Overflow of
-- ANY row, disabled included, aborts the entire transaction. Never two updates.
UPDATE public.monitor_activation_states SET desired_state='disabled',activation_configuration_version=NULL,
 activation_generation=activation_generation+1,last_transition_kind='upgrade_revoked',last_actor_user_id=NULL,
 updated_at=GREATEST(updated_at,statement_timestamp());

CREATE ROLE devpilot_scheduler_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE devpilot_scheduler_controller NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE devpilot_scheduler_worker NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE devpilot_scheduler_supervisor NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE ROLE devpilot_scheduler_api_owner NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;
CREATE ROLE devpilot_scheduler_api NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
CREATE SCHEMA scheduler_private AUTHORIZATION devpilot_scheduler_owner;
REVOKE ALL ON SCHEMA scheduler_private FROM PUBLIC;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
-- Runtime SECURITY DEFINER functions contain no dynamic SQL. Database-specific
-- bootstrap privilege removal is necessarily resolved once by the migration.
-- +goose StatementBegin
DO $$ BEGIN
 EXECUTE format('REVOKE CREATE, TEMPORARY ON DATABASE %I FROM PUBLIC',current_database());
END $$;
-- +goose StatementEnd

CREATE TABLE public.monitor_schedules (
 workspace_id uuid NOT NULL, project_id uuid NOT NULL, monitor_id uuid NOT NULL,
 authorized_configuration_version bigint CHECK(authorized_configuration_version>0),
 authorized_activation_generation bigint CHECK(authorized_activation_generation>0),
 consent_protocol smallint CHECK(consent_protocol=1),
 issued_at timestamptz, expires_at timestamptz,
 scheduler_epoch bigint NOT NULL DEFAULT 1 CHECK(scheduler_epoch>0),
 last_fencing_token bigint NOT NULL DEFAULT 0 CHECK(last_fencing_token>=0),
 -- A durable cycle identity survives an unknown/rolled-back Claim. Competing
 -- workers use this same run id instead of inventing a replacement for the due.
 next_run_id uuid NOT NULL DEFAULT uuidv7() UNIQUE,
 next_due_at timestamptz, last_started_at timestamptz, next_start_not_before timestamptz,
 last_attempt_run_id uuid, latest_result_run_id uuid,
 blocked_reason text CHECK(blocked_reason IN ('scheduler_unavailable','authorization_required','authorization_expired','owner_unavailable','egress_unavailable','emergency_stop','resource_limited','retention_capacity','quarantined','clock_untrusted')),
 created_at timestamptz NOT NULL DEFAULT statement_timestamp(), updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
 PRIMARY KEY(workspace_id,project_id,monitor_id),
 FOREIGN KEY(workspace_id,project_id,monitor_id) REFERENCES public.monitor_activation_states ON DELETE RESTRICT,
 CHECK(updated_at>=created_at),
 CHECK((authorized_configuration_version IS NULL AND authorized_activation_generation IS NULL AND consent_protocol IS NULL AND issued_at IS NULL AND expires_at IS NULL AND next_due_at IS NULL)
 OR (authorized_configuration_version IS NOT NULL AND authorized_activation_generation IS NOT NULL AND consent_protocol IS NOT NULL AND consent_protocol=1 AND issued_at IS NOT NULL AND expires_at IS NOT NULL AND expires_at>issued_at AND expires_at<=issued_at+interval '24 hours')),
 CHECK(next_start_not_before IS NULL OR last_started_at IS NULL OR next_start_not_before>=last_started_at)
);
CREATE INDEX monitor_schedules_due_idx ON public.monitor_schedules(next_due_at,workspace_id,project_id,monitor_id) WHERE next_due_at IS NOT NULL;

CREATE TABLE public.monitor_runs (
 workspace_id uuid NOT NULL,project_id uuid NOT NULL,monitor_id uuid NOT NULL,run_id uuid NOT NULL,
 configuration_version bigint NOT NULL CHECK(configuration_version>0),activation_generation bigint NOT NULL CHECK(activation_generation>0),
 scheduler_epoch bigint NOT NULL CHECK(scheduler_epoch>0),fencing_token bigint NOT NULL CHECK(fencing_token>0),
 scheduled_at timestamptz NOT NULL,claimed_at timestamptz NOT NULL,started_at timestamptz,finished_at timestamptz,
 -- Only the creating, uncommitted Claim may finalize claimed_at after all
 -- budget/control waits. xid8 does not wrap/reuse the 32-bit transaction id.
 creator_transaction xid8 NOT NULL DEFAULT pg_current_xact_id(),
 lease_owner name NOT NULL,worker_incarnation uuid NOT NULL,lease_until timestamptz NOT NULL,
 attempt_state text NOT NULL CHECK(attempt_state IN ('leased','started','completed','canceled','expired','unknown')),
 result_commit_id uuid,duration_ms integer CHECK(duration_ms BETWEEN 0 AND 30000),http_status integer CHECK(http_status BETWEEN 200 AND 599),success boolean,
 failure_code text CHECK(failure_code IN ('dns_failed','blocked_target','connect_failed','tls_failed','timeout','canceled','response_limit','protocol_error','unexpected_status')),
 physical_release_confirmed_at timestamptz,
 stop_reason text CHECK(stop_reason IN ('authorization_revoked','authorization_expired','lease_expired','commit_unknown','worker_lost','resource_limited','invalid_configuration','egress_unavailable','emergency_stop','retention_capacity')),
 PRIMARY KEY(workspace_id,project_id,monitor_id,run_id),UNIQUE(run_id),UNIQUE(workspace_id,project_id,monitor_id,fencing_token),
 FOREIGN KEY(workspace_id,project_id,monitor_id) REFERENCES public.monitor_schedules ON DELETE RESTRICT,
 CHECK(claimed_at>=scheduled_at AND lease_until>claimed_at),
 CHECK(started_at IS NULL OR started_at>=claimed_at),
 CHECK(finished_at IS NULL OR finished_at>=COALESCE(started_at,claimed_at)),
 CHECK(physical_release_confirmed_at IS NULL OR physical_release_confirmed_at>=claimed_at),
 CHECK((attempt_state='leased' AND started_at IS NULL AND finished_at IS NULL)
 OR (attempt_state='started' AND started_at IS NOT NULL AND finished_at IS NULL)
 OR (attempt_state IN ('completed','unknown') AND started_at IS NOT NULL AND finished_at IS NOT NULL)
 OR (attempt_state IN ('canceled','expired') AND finished_at IS NOT NULL)),
 CHECK((attempt_state='completed' AND result_commit_id IS NOT NULL AND duration_ms IS NOT NULL AND success IS NOT NULL
  AND ((success AND http_status IS NOT NULL AND failure_code IS NULL) OR (NOT success AND failure_code IS NOT NULL)))
 OR (attempt_state<>'completed' AND result_commit_id IS NULL AND duration_ms IS NULL AND http_status IS NULL AND success IS NULL AND failure_code IS NULL))
);
CREATE UNIQUE INDEX monitor_runs_outstanding_idx ON public.monitor_runs(workspace_id,project_id,monitor_id)
 WHERE attempt_state IN ('leased','started') OR physical_release_confirmed_at IS NULL;
CREATE INDEX monitor_runs_lease_idx ON public.monitor_runs(lease_until,workspace_id,project_id,monitor_id) WHERE attempt_state IN ('leased','started');
CREATE INDEX monitor_runs_retention_idx ON public.monitor_runs(finished_at,workspace_id,project_id,monitor_id,run_id) WHERE finished_at IS NOT NULL;
ALTER TABLE public.monitor_schedules ADD CONSTRAINT monitor_schedules_last_attempt_fk FOREIGN KEY(workspace_id,project_id,monitor_id,last_attempt_run_id) REFERENCES public.monitor_runs ON DELETE RESTRICT;
ALTER TABLE public.monitor_schedules ADD CONSTRAINT monitor_schedules_latest_result_fk FOREIGN KEY(workspace_id,project_id,monitor_id,latest_result_run_id) REFERENCES public.monitor_runs ON DELETE RESTRICT;

CREATE TABLE public.scheduler_budget_state (
 kind text NOT NULL CHECK(kind IN ('global','workspace','project','monitor','origin','ip')),budget_key text NOT NULL,
 in_use integer NOT NULL DEFAULT 0 CHECK(in_use>=0),tokens bigint NOT NULL DEFAULT 0 CHECK(tokens>=0),
 last_refill_at timestamptz NOT NULL DEFAULT statement_timestamp(),not_before timestamptz,keep_until timestamptz NOT NULL,
 terminal_count integer NOT NULL DEFAULT 0 CHECK(terminal_count BETWEEN 0 AND 100000),
 active_count integer NOT NULL DEFAULT 0 CHECK(active_count BETWEEN 0 AND 32),
 schedule_count integer NOT NULL DEFAULT 0 CHECK(schedule_count BETWEEN 0 AND 10000),
 key_count integer NOT NULL DEFAULT 0 CHECK(key_count BETWEEN 0 AND 10000),
 PRIMARY KEY(kind,budget_key),
 CHECK(in_use<=CASE kind WHEN 'global' THEN 32 WHEN 'workspace' THEN 4 WHEN 'project' THEN 2 WHEN 'monitor' THEN 1 WHEN 'origin' THEN 1 WHEN 'ip' THEN 2 END),
 CHECK(tokens<=CASE kind WHEN 'global' THEN 8000000 WHEN 'workspace' THEN 2000000 ELSE 1000000 END),
 CHECK((kind='global' AND budget_key='global') OR (kind IN ('origin','ip') AND budget_key ~ '^[0-9a-f]{64}$')
 OR (kind='workspace' AND budget_key ~ '^[0-9a-f-]{36}$') OR (kind='project' AND budget_key ~ '^[0-9a-f-]{36}/[0-9a-f-]{36}$')
 OR (kind='monitor' AND budget_key ~ '^[0-9a-f-]{36}/[0-9a-f-]{36}/[0-9a-f-]{36}$')),
 CHECK(kind='global' OR (terminal_count=0 AND active_count=0 AND schedule_count=0 AND key_count=0))
);
INSERT INTO public.scheduler_budget_state(kind,budget_key,tokens,keep_until,key_count) VALUES('global','global',8000000,statement_timestamp()+interval '1 hour',1);

CREATE TABLE public.scheduler_reservations (
 workspace_id uuid NOT NULL,project_id uuid NOT NULL,monitor_id uuid NOT NULL,run_id uuid NOT NULL,reservation_id uuid NOT NULL DEFAULT uuidv7(),
 reservation_state text NOT NULL CHECK(reservation_state IN ('reserved','quarantined','released')),
 namespace_incarnation uuid,supervisor_incarnation uuid,permit_deadline timestamptz,
 origin_key text CHECK(origin_key ~ '^[0-9a-f]{64}$'),ip_key text CHECK(ip_key ~ '^[0-9a-f]{64}$'),
 termination_receipt bytea CHECK(octet_length(termination_receipt)=32),released_at timestamptz,
 PRIMARY KEY(workspace_id,project_id,monitor_id,run_id),UNIQUE(reservation_id),
 FOREIGN KEY(workspace_id,project_id,monitor_id,run_id) REFERENCES public.monitor_runs ON DELETE RESTRICT,
 CHECK((namespace_incarnation IS NULL)=(supervisor_incarnation IS NULL)),
 CHECK((reservation_state='released' AND released_at IS NOT NULL AND termination_receipt IS NOT NULL)
 OR (reservation_state<>'released' AND released_at IS NULL AND termination_receipt IS NULL))
);
CREATE TABLE public.scheduler_control (
 control_id text PRIMARY KEY,kind text NOT NULL CHECK(kind IN ('deployment','worker','http','supervisor')),
 login_name name,incarnation uuid,
 gate text NOT NULL DEFAULT 'closed' CHECK(gate IN ('closed','fixture')),emergency_stop boolean NOT NULL DEFAULT true,
 expires_at timestamptz,verified_at timestamptz,manifest_digest bytea CHECK(octet_length(manifest_digest)=32),
 projection_protocol text CHECK(projection_protocol='intent-v2'),status_ready boolean NOT NULL DEFAULT false,routable boolean NOT NULL DEFAULT false,
 disk_used_bytes bigint NOT NULL DEFAULT 0 CHECK(disk_used_bytes>=0),disk_budget_bytes bigint NOT NULL DEFAULT 1073741824 CHECK(disk_budget_bytes=1073741824),
 CHECK((kind='deployment' AND control_id='deployment' AND login_name IS NULL AND incarnation IS NULL)
 OR(kind<>'deployment' AND login_name IS NOT NULL AND incarnation IS NOT NULL)),
 CHECK(gate='closed' OR (kind='deployment' AND verified_at IS NOT NULL AND expires_at IS NOT NULL AND manifest_digest IS NOT NULL AND NOT emergency_stop)),
 UNIQUE(kind,login_name,incarnation)
);
INSERT INTO public.scheduler_control(control_id,kind) VALUES('deployment','deployment');

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.activation_revoke() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF NEW.activation_generation IS DISTINCT FROM OLD.activation_generation THEN
  UPDATE public.monitor_schedules SET authorized_configuration_version=NULL,authorized_activation_generation=NULL,
   consent_protocol=NULL,issued_at=NULL,expires_at=NULL,next_due_at=NULL,scheduler_epoch=scheduler_epoch+1,
   blocked_reason='authorization_required',next_run_id=uuidv7(),updated_at=GREATEST(updated_at,clock_timestamp())
  WHERE workspace_id=NEW.workspace_id AND project_id=NEW.project_id AND monitor_id=NEW.monitor_id;
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.activation_revoke() OWNER TO devpilot_scheduler_owner;
CREATE TRIGGER scheduler_activation_revoke AFTER UPDATE ON public.monitor_activation_states FOR EACH ROW EXECUTE FUNCTION scheduler_private.activation_revoke();

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.schedule_guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF ROW(NEW.workspace_id,NEW.project_id,NEW.monitor_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.monitor_id,OLD.created_at)
 OR NEW.scheduler_epoch<OLD.scheduler_epoch OR NEW.last_fencing_token<OLD.last_fencing_token OR NEW.updated_at<OLD.updated_at
 OR (OLD.last_started_at IS NOT NULL AND (NEW.last_started_at IS NULL OR NEW.last_started_at<OLD.last_started_at))
 OR (OLD.next_start_not_before IS NOT NULL AND (NEW.next_start_not_before IS NULL OR NEW.next_start_not_before<OLD.next_start_not_before)) THEN
  RAISE EXCEPTION 'scheduler identity or monotonic boundary' USING ERRCODE='23514';
 END IF;
 IF ROW(NEW.authorized_configuration_version,NEW.authorized_activation_generation,NEW.consent_protocol,NEW.issued_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.authorized_configuration_version,OLD.authorized_activation_generation,OLD.consent_protocol,OLD.issued_at,OLD.expires_at)
 AND NEW.scheduler_epoch<>OLD.scheduler_epoch+1 THEN
  RAISE EXCEPTION 'authorization change requires next epoch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER scheduler_schedule_guard BEFORE UPDATE ON public.monitor_schedules FOR EACH ROW EXECUTE FUNCTION scheduler_private.schedule_guard();

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.run_guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.creator_transaction IS DISTINCT FROM pg_current_xact_id() THEN
   RAISE EXCEPTION 'run creator transaction is not current' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.claimed_at IS DISTINCT FROM OLD.claimed_at AND
  (OLD.creator_transaction<>pg_current_xact_id() OR OLD.attempt_state<>'leased' OR NEW.attempt_state<>'leased' OR OLD.started_at IS NOT NULL) THEN
  RAISE EXCEPTION 'committed claim time is immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.attempt_state IN ('completed','canceled','expired','unknown') AND NEW.lease_until IS DISTINCT FROM OLD.lease_until THEN
  RAISE EXCEPTION 'terminal lease is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.physical_release_confirmed_at IS NOT NULL AND OLD.physical_release_confirmed_at IS NULL
 AND NOT pg_has_role(session_user,'devpilot_scheduler_supervisor','MEMBER')
 AND NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname=session_user AND r.rolsuper) THEN
  RAISE EXCEPTION 'independent supervisor proof required' USING ERRCODE='42501';
 END IF;
 IF ROW(NEW.workspace_id,NEW.project_id,NEW.monitor_id,NEW.run_id,NEW.configuration_version,NEW.activation_generation,NEW.scheduler_epoch,NEW.fencing_token,NEW.scheduled_at,NEW.creator_transaction,NEW.lease_owner,NEW.worker_incarnation)
 IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.monitor_id,OLD.run_id,OLD.configuration_version,OLD.activation_generation,OLD.scheduler_epoch,OLD.fencing_token,OLD.scheduled_at,OLD.creator_transaction,OLD.lease_owner,OLD.worker_incarnation)
 OR (OLD.started_at IS NOT NULL AND NEW.started_at IS DISTINCT FROM OLD.started_at)
 OR (OLD.finished_at IS NOT NULL AND NEW.finished_at IS DISTINCT FROM OLD.finished_at)
 OR NEW.lease_until<OLD.lease_until
 OR (OLD.physical_release_confirmed_at IS NOT NULL AND NEW.physical_release_confirmed_at IS DISTINCT FROM OLD.physical_release_confirmed_at) THEN
  RAISE EXCEPTION 'run lineage or time is immutable' USING ERRCODE='23514';
 END IF;
 IF (OLD.attempt_state IN ('completed','canceled','expired','unknown') AND
 ROW(NEW.attempt_state,NEW.result_commit_id,NEW.duration_ms,NEW.http_status,NEW.success,NEW.failure_code,NEW.stop_reason)
 IS DISTINCT FROM ROW(OLD.attempt_state,OLD.result_commit_id,OLD.duration_ms,OLD.http_status,OLD.success,OLD.failure_code,OLD.stop_reason))
 OR (OLD.attempt_state='started' AND NEW.attempt_state IN ('leased','expired')) THEN
  RAISE EXCEPTION 'run cannot restart or rewrite a terminal result' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER scheduler_run_guard BEFORE INSERT OR UPDATE ON public.monitor_runs FOR EACH ROW EXECUTE FUNCTION scheduler_private.run_guard();

-- Private owner has no password/session data. UPDATE(id) allows PG row locks;
-- no runtime principal inherits this non-LOGIN, NOINHERIT owner role.
GRANT USAGE ON SCHEMA public TO devpilot_scheduler_owner;
GRANT SELECT(id,status),UPDATE(id) ON public.users TO devpilot_scheduler_owner;
GRANT SELECT(id),UPDATE(id) ON public.workspaces TO devpilot_scheduler_owner;
GRANT SELECT(workspace_id,user_id,membership_id,role),UPDATE(role) ON public.workspace_members TO devpilot_scheduler_owner;
GRANT SELECT(workspace_id,id),UPDATE(id) ON public.projects TO devpilot_scheduler_owner;
GRANT SELECT,UPDATE(id) ON public.http_monitors TO devpilot_scheduler_owner;
GRANT SELECT,UPDATE(monitor_id) ON public.monitor_activation_states TO devpilot_scheduler_owner;
GRANT SELECT,UPDATE ON public.monitor_schedules,public.monitor_runs,public.scheduler_budget_state,public.scheduler_reservations,public.scheduler_control TO devpilot_scheduler_owner;
GRANT INSERT,DELETE ON public.monitor_schedules,public.monitor_runs,public.scheduler_budget_state,public.scheduler_reservations TO devpilot_scheduler_owner;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA scheduler_private FROM PUBLIC;
GRANT USAGE ON SCHEMA public,scheduler_private TO devpilot_scheduler_api_owner;
GRANT SELECT(id,status),UPDATE(id) ON public.users TO devpilot_scheduler_api_owner;
GRANT SELECT(token_hash,user_id,expires_at,revoked_at),UPDATE(token_hash) ON public.sessions TO devpilot_scheduler_api_owner;
GRANT SELECT(id),UPDATE(id) ON public.workspaces TO devpilot_scheduler_api_owner;
GRANT SELECT(workspace_id,user_id,membership_id,role),UPDATE(role) ON public.workspace_members TO devpilot_scheduler_api_owner;
GRANT SELECT(workspace_id,id),UPDATE(id) ON public.projects TO devpilot_scheduler_api_owner;
GRANT SELECT,UPDATE(id) ON public.http_monitors TO devpilot_scheduler_api_owner;
GRANT SELECT,UPDATE ON public.monitor_activation_states,public.monitor_schedules,public.scheduler_budget_state TO devpilot_scheduler_api_owner;
GRANT INSERT ON public.monitor_schedules TO devpilot_scheduler_api_owner;
GRANT SELECT,UPDATE(control_id) ON public.scheduler_control TO devpilot_scheduler_api_owner;
GRANT USAGE ON SCHEMA scheduler_private TO devpilot_scheduler_controller,devpilot_scheduler_worker,devpilot_scheduler_supervisor,devpilot_scheduler_api;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.schedule_capacity() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  UPDATE public.scheduler_budget_state SET schedule_count=schedule_count+1 WHERE kind='global' AND budget_key='global' AND schedule_count<10000;
  IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF OLD.expires_at>clock_timestamp() OR OLD.next_start_not_before>clock_timestamp() OR OLD.last_started_at>clock_timestamp()-interval '1 hour'
 OR EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=OLD.workspace_id AND r.project_id=OLD.project_id AND r.monitor_id=OLD.monitor_id) THEN
  RAISE EXCEPTION 'schedule still owns authorization history or cooling' USING ERRCODE='23514';
 END IF;
 UPDATE public.scheduler_budget_state SET schedule_count=schedule_count-1 WHERE kind='global' AND budget_key='global';
 RETURN OLD;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.schedule_capacity() OWNER TO devpilot_scheduler_owner;
CREATE TRIGGER scheduler_schedule_capacity AFTER INSERT ON public.monitor_schedules FOR EACH ROW EXECUTE FUNCTION scheduler_private.schedule_capacity();
CREATE TRIGGER scheduler_schedule_capacity_delete BEFORE DELETE ON public.monitor_schedules FOR EACH ROW EXECUTE FUNCTION scheduler_private.schedule_capacity();

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.budget_capacity() RETURNS trigger LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.kind,NEW.budget_key) IS DISTINCT FROM ROW(OLD.kind,OLD.budget_key) OR NEW.last_refill_at<OLD.last_refill_at OR NEW.keep_until<OLD.keep_until
   OR (OLD.not_before IS NOT NULL AND (NEW.not_before IS NULL OR NEW.not_before<OLD.not_before)) THEN
   RAISE EXCEPTION 'budget identity clock or cooling is immutable' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='INSERT' THEN
  UPDATE public.scheduler_budget_state SET key_count=key_count+1 WHERE kind='global' AND budget_key='global' AND key_count<10000;
  IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF OLD.kind='global' OR OLD.in_use<>0 OR OLD.keep_until>clock_timestamp() OR OLD.not_before>clock_timestamp()
 OR EXISTS(SELECT 1 FROM public.scheduler_reservations r WHERE (OLD.kind='origin' AND r.origin_key=OLD.budget_key) OR (OLD.kind='ip' AND r.ip_key=OLD.budget_key))
 OR EXISTS(SELECT 1 FROM public.monitor_runs r WHERE (OLD.kind='workspace' AND r.workspace_id::text=OLD.budget_key)
  OR (OLD.kind='project' AND r.workspace_id::text||'/'||r.project_id::text=OLD.budget_key)
  OR (OLD.kind='monitor' AND r.workspace_id::text||'/'||r.project_id::text||'/'||r.monitor_id::text=OLD.budget_key)) THEN
  RAISE EXCEPTION 'budget still owns capacity history or cooling' USING ERRCODE='23514';
 END IF;
 UPDATE public.scheduler_budget_state SET key_count=key_count-1 WHERE kind='global' AND budget_key='global';
 RETURN OLD;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.budget_capacity() OWNER TO devpilot_scheduler_owner;
CREATE TRIGGER scheduler_budget_capacity AFTER INSERT ON public.scheduler_budget_state FOR EACH ROW EXECUTE FUNCTION scheduler_private.budget_capacity();
CREATE TRIGGER scheduler_budget_guard BEFORE UPDATE OR DELETE ON public.scheduler_budget_state FOR EACH ROW EXECUTE FUNCTION scheduler_private.budget_capacity();
REVOKE ALL ON FUNCTION scheduler_private.schedule_capacity(),scheduler_private.budget_capacity() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.authorize(p_digest bytea,p_wid uuid,p_pid uuid,p_mid uuid,p_tag_mid uuid,p_v bigint,p_g bigint,p_body_v bigint,p_seconds integer)
RETURNS TABLE(decision text,data jsonb,retry_after integer) LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
DECLARE
 uid uuid; user_status text; protected boolean:=false; member_role text; found_id uuid;
 current_v bigint; current_g bigint; old_desired text; cooling timestamptz;
 gate_now timestamptz; permitted boolean; auth_row public.monitor_schedules%ROWTYPE;
BEGIN
 IF p_seconds IS NULL OR p_seconds<60 OR p_seconds>86400 OR p_body_v IS NULL OR p_body_v<=0 OR octet_length(p_digest)<>32 THEN
  RAISE EXCEPTION 'invalid authorization input' USING ERRCODE='22023';
 END IF;
 SELECT s.user_id INTO uid FROM public.sessions s WHERE s.token_hash=p_digest;
 IF uid IS NULL THEN RETURN QUERY SELECT 'unauthenticated',NULL::jsonb,0; RETURN; END IF;
 SELECT u.status INTO user_status FROM public.users u WHERE u.id=uid FOR SHARE;
 IF user_status IS DISTINCT FROM 'active' THEN RETURN QUERY SELECT 'unauthenticated',NULL::jsonb,0; RETURN; END IF;
 PERFORM 1 FROM public.sessions s WHERE s.token_hash=p_digest AND s.user_id=uid AND s.revoked_at IS NULL FOR SHARE;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthenticated',NULL::jsonb,0; RETURN; END IF;
 SELECT w.id INTO found_id FROM public.workspaces w WHERE w.id=p_wid
  AND EXISTS(SELECT 1 FROM public.workspace_members b WHERE b.workspace_id=w.id AND b.user_id=uid) FOR NO KEY UPDATE;
 protected:=FOUND;
 IF protected THEN
  SELECT b.role INTO member_role FROM public.workspace_members b WHERE b.workspace_id=p_wid AND b.user_id=uid FOR UPDATE;
  IF member_role='owner' THEN
   PERFORM 1 FROM public.projects p WHERE p.workspace_id=p_wid AND p.id=p_pid FOR UPDATE;
   IF FOUND THEN
    PERFORM 1 FROM public.http_monitors m WHERE m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid FOR UPDATE;
    IF FOUND THEN
     PERFORM 1 FROM public.monitor_activation_states a WHERE a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid FOR UPDATE;
     IF NOT FOUND THEN RAISE EXCEPTION 'activation missing' USING ERRCODE='23514'; END IF;
     PERFORM 1 FROM public.monitor_schedules s WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid FOR UPDATE;
     -- Capacity first in the fixed budget order. Do not wait for this after
     -- checking session expiry. Operational callers cannot take it then a parent.
     IF NOT FOUND THEN
      PERFORM 1 FROM public.scheduler_budget_state b WHERE b.kind='global' AND b.budget_key='global' FOR UPDATE;
     END IF;
    END IF;
   END IF;
  END IF;
 END IF;
 PERFORM 1 FROM public.scheduler_control c WHERE c.control_id='deployment' FOR SHARE;
 -- New SPI statement after ALL waits, fresh READ COMMITTED snapshot and time.
 gate_now:=clock_timestamp();
 PERFORM 1 FROM public.sessions s JOIN public.users u ON u.id=s.user_id WHERE s.token_hash=p_digest AND u.id=uid
  AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>gate_now;
 IF NOT FOUND THEN RETURN QUERY SELECT 'unauthenticated',NULL::jsonb,0; RETURN; END IF;
 SELECT b.role,m.version,a.activation_generation,a.desired_state,a.next_enable_allowed_at
 INTO member_role,current_v,current_g,old_desired,cooling
 FROM public.workspace_members b JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 WHERE b.workspace_id=p_wid AND b.user_id=uid AND protected;
 IF NOT FOUND THEN RETURN QUERY SELECT 'not_found',NULL::jsonb,0; RETURN; END IF;
 IF member_role<>'owner' THEN RETURN QUERY SELECT 'forbidden',NULL::jsonb,0; RETURN; END IF;
 IF p_g IS NULL THEN RETURN QUERY SELECT 'required',NULL::jsonb,0; RETURN; END IF;
 IF p_tag_mid IS DISTINCT FROM p_mid OR current_v IS DISTINCT FROM p_v OR current_g IS DISTINCT FROM p_g OR current_v<>p_body_v THEN
  RETURN QUERY SELECT 'failed',NULL::jsonb,0; RETURN;
 END IF;
 IF current_g=9223372036854775807 THEN RAISE EXCEPTION 'generation exhausted' USING ERRCODE='22003'; END IF;
 IF old_desired<>'enabled' AND (SELECT count(*) FROM public.monitor_activation_states a WHERE a.workspace_id=p_wid AND a.desired_state='enabled')>=100 THEN
  RETURN QUERY SELECT 'limit',NULL::jsonb,0; RETURN;
 END IF;
 IF cooling>gate_now THEN RETURN QUERY SELECT 'cooling',NULL::jsonb,GREATEST(1,ceil(extract(epoch FROM cooling-gate_now))::integer); RETURN; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.monitor_schedules s WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid)
 AND (SELECT b.schedule_count FROM public.scheduler_budget_state b WHERE b.kind='global' AND b.budget_key='global')>=10000 THEN
  RETURN QUERY SELECT 'retention_capacity',NULL::jsonb,0; RETURN;
 END IF;
 SELECT c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now AND c.disk_used_bytes<c.disk_budget_bytes*0.8
 INTO permitted FROM public.scheduler_control c WHERE c.control_id='deployment';
 UPDATE public.monitor_activation_states a SET desired_state='enabled',activation_configuration_version=current_v,
  activation_generation=current_g+1,next_enable_allowed_at=GREATEST(a.next_enable_allowed_at,gate_now+interval '60 seconds'),
  last_transition_kind='owner_enabled',last_actor_user_id=uid,updated_at=GREATEST(a.updated_at,gate_now)
 WHERE a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid AND a.activation_generation=current_g;
 INSERT INTO public.monitor_schedules(workspace_id,project_id,monitor_id,authorized_configuration_version,authorized_activation_generation,consent_protocol,issued_at,expires_at,next_due_at,blocked_reason,created_at,updated_at)
 VALUES(p_wid,p_pid,p_mid,current_v,current_g+1,1,gate_now,gate_now+p_seconds*interval '1 second',CASE WHEN permitted THEN gate_now END,CASE WHEN permitted THEN NULL ELSE 'scheduler_unavailable' END,gate_now,gate_now)
 ON CONFLICT(workspace_id,project_id,monitor_id) DO UPDATE SET authorized_configuration_version=EXCLUDED.authorized_configuration_version,
  authorized_activation_generation=EXCLUDED.authorized_activation_generation,consent_protocol=1,issued_at=gate_now,expires_at=EXCLUDED.expires_at,
  scheduler_epoch=public.monitor_schedules.scheduler_epoch+1,next_run_id=uuidv7(),next_due_at=CASE WHEN permitted THEN GREATEST(gate_now,public.monitor_schedules.next_start_not_before) END,
  blocked_reason=EXCLUDED.blocked_reason,updated_at=GREATEST(public.monitor_schedules.updated_at,gate_now)
 RETURNING * INTO auth_row;
 RETURN QUERY SELECT 'ok',jsonb_build_object('workspace_id',p_wid,'project_id',p_pid,'monitor_id',p_mid,
  'configuration_version',current_v::text,'activation_generation',(current_g+1)::text,'scheduler_epoch',auth_row.scheduler_epoch::text,
  'issued_at',gate_now,'expires_at',auth_row.expires_at,'effective_authorization',COALESCE(permitted,false),'blocked_reason',auth_row.blocked_reason),0;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.authorize(bytea,uuid,uuid,uuid,uuid,bigint,bigint,bigint,integer) OWNER TO devpilot_scheduler_api_owner;
REVOKE ALL ON FUNCTION scheduler_private.authorize(bytea,uuid,uuid,uuid,uuid,bigint,bigint,bigint,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.authorize(bytea,uuid,uuid,uuid,uuid,bigint,bigint,bigint,integer) TO devpilot_scheduler_api;

GRANT SELECT ON public.monitor_runs TO devpilot_scheduler_api_owner;
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.execution_status(p_digest bytea,p_wid uuid,p_pid uuid,p_mid uuid)
RETURNS TABLE(decision text,data jsonb) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
WITH gate_time AS MATERIALIZED(SELECT clock_timestamp() AS now), identity AS MATERIALIZED (
 SELECT u.id FROM public.users u JOIN public.sessions s ON s.user_id=u.id CROSS JOIN gate_time t
 WHERE s.token_hash=p_digest AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>t.now
), scoped AS MATERIALIZED (
 SELECT m.workspace_id,m.project_id,m.id monitor_id,m.version,a.activation_generation,a.desired_state,
 s.authorized_configuration_version,s.authorized_activation_generation,s.scheduler_epoch,s.expires_at,s.next_due_at,
 c.gate,c.emergency_stop,c.expires_at deployment_expires,c.disk_used_bytes,c.disk_budget_bytes,t.now,
 EXISTS(SELECT 1 FROM public.workspace_members o JOIN public.users u ON u.id=o.user_id WHERE o.workspace_id=m.workspace_id AND o.role='owner' AND u.status='active') has_owner,
 la.attempt,lc.observation,
 la.v attempt_v,la.g attempt_g,la.e attempt_e,la.state attempt_state,
 lc.v completed_v,lc.g completed_g,lc.e completed_e,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.started_at IS NOT NULL AND r.physical_release_confirmed_at IS NULL) in_flight,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state IN ('unknown','completed') AND r.physical_release_confirmed_at IS NULL) quarantined
 FROM identity i JOIN public.workspace_members b ON b.user_id=i.id AND b.workspace_id=p_wid
 JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 LEFT JOIN public.monitor_schedules s ON s.workspace_id=m.workspace_id AND s.project_id=m.project_id AND s.monitor_id=m.id
 CROSS JOIN gate_time t CROSS JOIN public.scheduler_control c
 LEFT JOIN LATERAL (
  SELECT jsonb_build_object('workspace_id',r.workspace_id,'project_id',r.project_id,'monitor_id',r.monitor_id,'run_id',r.run_id,
   'configuration_version',r.configuration_version::text,'activation_generation',r.activation_generation::text,'scheduler_epoch',r.scheduler_epoch::text,'fencing_token',r.fencing_token::text,
   'attempt_state',r.attempt_state,'scheduled_at',r.scheduled_at,'claimed_at',r.claimed_at,'started_at',r.started_at,'finished_at',r.finished_at,
   'duration_ms',r.duration_ms,'http_status',r.http_status,'success',r.success,'failure_code',r.failure_code,'stop_reason',r.stop_reason) attempt,
   r.configuration_version v,r.activation_generation g,r.scheduler_epoch e,r.attempt_state state
  FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id
  ORDER BY r.claimed_at DESC,r.run_id DESC LIMIT 1
 ) la ON true
 LEFT JOIN LATERAL (
  SELECT jsonb_build_object('workspace_id',r.workspace_id,'project_id',r.project_id,'monitor_id',r.monitor_id,'run_id',r.run_id,
   'configuration_version',r.configuration_version::text,'activation_generation',r.activation_generation::text,'scheduler_epoch',r.scheduler_epoch::text,'fencing_token',r.fencing_token::text,
   'attempt_state',r.attempt_state,'scheduled_at',r.scheduled_at,'claimed_at',r.claimed_at,'started_at',r.started_at,'finished_at',r.finished_at,
   'duration_ms',r.duration_ms,'http_status',r.http_status,'success',r.success,'failure_code',r.failure_code,'stop_reason',r.stop_reason) observation,
   r.configuration_version v,r.activation_generation g,r.scheduler_epoch e
  FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state='completed'
  ORDER BY r.finished_at DESC,r.run_id DESC LIMIT 1
 ) lc ON true
 WHERE c.control_id='deployment'
), states AS MATERIALIZED (
 SELECT *,CASE WHEN authorized_configuration_version IS NULL THEN 'none' WHEN expires_at<=now THEN 'expired' ELSE 'valid' END authorization_state,
 CASE WHEN desired_state='disabled' THEN NULL
 WHEN authorized_configuration_version IS NULL OR authorized_configuration_version<>version OR authorized_activation_generation<>activation_generation THEN 'authorization_required'
 WHEN expires_at<=now THEN 'authorization_expired'
 WHEN NOT has_owner THEN 'owner_unavailable'
 WHEN gate<>'fixture' THEN 'scheduler_unavailable'
 WHEN emergency_stop THEN 'emergency_stop'
 WHEN deployment_expires<=now OR deployment_expires IS NULL THEN 'egress_unavailable'
 WHEN disk_used_bytes>=disk_budget_bytes*0.8 THEN 'retention_capacity'
 WHEN quarantined THEN 'quarantined' ELSE NULL END reason
 FROM scoped
)
SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM identity) THEN 'unauthenticated' WHEN NOT EXISTS(SELECT 1 FROM scoped) THEN 'not_found' ELSE 'ok' END,
 (SELECT jsonb_build_object('workspace_id',workspace_id,'project_id',project_id,'monitor_id',monitor_id,
 'configuration_version',version::text,'activation_generation',activation_generation::text,'scheduler_epoch',scheduler_epoch::text,
 'desired_state',desired_state,'authorization_state',authorization_state,'scheduling_state',CASE WHEN desired_state='disabled' THEN 'disabled' WHEN reason IS NOT NULL THEN 'blocked' WHEN next_due_at<=now THEN 'due' ELSE 'authorized_waiting' END,
 'effective_authorization',desired_state='enabled' AND authorization_state='valid' AND reason IS NULL,
 'expires_at',expires_at,'next_due',next_due_at,'in_flight',in_flight,'pause_semantics','start_commit','blocked_reason',reason,
 'latest_attempt',attempt,'latest_completed_observation',observation,
 'current_observation',CASE WHEN attempt_state IS DISTINCT FROM 'unknown' AND completed_v=version AND completed_g=activation_generation AND completed_e=scheduler_epoch THEN observation END) FROM states);
$$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) OWNER TO devpilot_scheduler_api_owner;
REVOKE ALL ON FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) TO devpilot_scheduler_api;

-- System principals are provisioned by the trusted fixture bootstrap, never by
-- an API request or a worker-provided boolean. A new incarnation needs a new
-- trusted registration; reconnecting cannot manufacture or renew it.
ALTER TABLE public.scheduler_control ADD COLUMN last_transaction xid8;
ALTER TABLE public.scheduler_control ADD COLUMN last_gate_at timestamptz;
ALTER TABLE public.scheduler_budget_state DROP CONSTRAINT scheduler_budget_state_check3;
ALTER TABLE public.scheduler_budget_state ADD CONSTRAINT scheduler_budget_scope_counters CHECK(
 (kind='global') OR (kind IN ('workspace','monitor') AND active_count=0 AND schedule_count=0 AND key_count=0
 AND terminal_count<=CASE kind WHEN 'workspace' THEN 10000 ELSE 100 END)
 OR (kind NOT IN ('global','workspace','monitor') AND terminal_count=0 AND active_count=0 AND schedule_count=0 AND key_count=0));

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.system_identity(p_kind text,p_incarnation uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF p_kind NOT IN ('worker','controller','supervisor') OR p_kind IS NULL OR p_incarnation IS NULL
 OR NOT EXISTS(SELECT 1 FROM pg_catalog.pg_roles r WHERE r.rolname=session_user AND r.rolcanlogin
 AND NOT(r.rolsuper OR r.rolbypassrls OR r.rolcreaterole OR r.rolreplication OR r.rolcreatedb))
 OR NOT (CASE p_kind WHEN 'worker' THEN pg_has_role(session_user,'devpilot_scheduler_worker','MEMBER')
 WHEN 'controller' THEN pg_has_role(session_user,'devpilot_scheduler_controller','MEMBER')
 ELSE pg_has_role(session_user,'devpilot_scheduler_supervisor','MEMBER') END)
 OR NOT EXISTS(SELECT 1 FROM public.scheduler_control c WHERE c.kind=p_kind AND c.login_name=session_user
 AND c.incarnation=p_incarnation AND c.verified_at IS NOT NULL AND c.expires_at>clock_timestamp() AND NOT c.emergency_stop)
 THEN RAISE EXCEPTION 'system identity unavailable' USING ERRCODE='42501'; END IF;
 IF current_setting('transaction_isolation')<>'read committed'
 OR current_setting('statement_timeout')::interval<=interval '0 seconds'
 OR current_setting('statement_timeout')::interval>interval '2 seconds'
 OR current_setting('transaction_timeout')::interval<=interval '0 seconds'
 OR current_setting('transaction_timeout')::interval>interval '2 seconds'
 OR clock_timestamp()>transaction_timestamp()+interval '2 seconds' THEN
  RAISE EXCEPTION 'bounded read committed transaction required' USING ERRCODE='25000';
 END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.system_identity(text,uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.system_identity(text,uuid) FROM PUBLIC;

-- This helper is private: no runtime LOGIN can call it, select an arbitrary
-- owner half way through a claim, or acquire children without their parents.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.lock_parents(p_wid uuid,p_pid uuid,p_mid uuid,p_uid uuid,p_membership uuid,p_skip boolean)
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE user_status text; member_id uuid; member_role text;
BEGIN
 IF p_uid IS NOT NULL THEN
  SELECT u.status INTO user_status FROM public.users u WHERE u.id=p_uid FOR SHARE;
  IF user_status IS DISTINCT FROM 'active' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 END IF;
 IF p_skip THEN
  PERFORM 1 FROM public.workspaces w WHERE w.id=p_wid FOR NO KEY UPDATE SKIP LOCKED;
 ELSE
  PERFORM 1 FROM public.workspaces w WHERE w.id=p_wid FOR NO KEY UPDATE;
 END IF;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_busy' USING ERRCODE='P0001'; END IF;
 IF p_uid IS NOT NULL THEN
  SELECT b.membership_id,b.role INTO member_id,member_role FROM public.workspace_members b WHERE b.workspace_id=p_wid AND b.user_id=p_uid FOR UPDATE;
  IF member_id IS DISTINCT FROM p_membership OR member_role IS DISTINCT FROM 'owner' THEN
   RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001';
  END IF;
 END IF;
 PERFORM 1 FROM public.projects p WHERE p.workspace_id=p_wid AND p.id=p_pid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.http_monitors m WHERE m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.monitor_activation_states a WHERE a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 IF p_skip THEN
  PERFORM 1 FROM public.monitor_schedules s WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid FOR UPDATE SKIP LOCKED;
 ELSE
  PERFORM 1 FROM public.monitor_schedules s WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid FOR UPDATE;
 END IF;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_busy' USING ERRCODE='P0001'; END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.lock_parents(uuid,uuid,uuid,uuid,uuid,boolean) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.lock_parents(uuid,uuid,uuid,uuid,uuid,boolean) FROM PUBLIC;

-- All callers hold Run before these locks. The order is explicit, not the
-- planner-dependent order of a joined locking SELECT. Counts reserve future
-- terminal capacity at admission so a later unknown never loses its audit slot.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.lock_budgets(p_wid uuid,p_pid uuid,p_mid uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE n timestamptz:=clock_timestamp();
BEGIN
 PERFORM 1 FROM public.scheduler_budget_state b WHERE b.kind='global' AND b.budget_key='global' FOR UPDATE;
 INSERT INTO public.scheduler_budget_state(kind,budget_key,tokens,keep_until) VALUES('workspace',p_wid::text,2000000,n+interval '1 hour') ON CONFLICT DO NOTHING;
 PERFORM 1 FROM public.scheduler_budget_state b WHERE b.kind='workspace' AND b.budget_key=p_wid::text FOR UPDATE;
 INSERT INTO public.scheduler_budget_state(kind,budget_key,tokens,keep_until) VALUES('project',p_wid::text||'/'||p_pid::text,1000000,n+interval '1 hour') ON CONFLICT DO NOTHING;
 PERFORM 1 FROM public.scheduler_budget_state b WHERE b.kind='project' AND b.budget_key=p_wid::text||'/'||p_pid::text FOR UPDATE;
 INSERT INTO public.scheduler_budget_state(kind,budget_key,tokens,keep_until) VALUES('monitor',p_wid::text||'/'||p_pid::text||'/'||p_mid::text,1000000,n+interval '1 hour') ON CONFLICT DO NOTHING;
 PERFORM 1 FROM public.scheduler_budget_state b WHERE b.kind='monitor' AND b.budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text FOR UPDATE;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.lock_budgets(uuid,uuid,uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.lock_budgets(uuid,uuid,uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.lock_control(p_incarnation uuid) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE previous xid8;
BEGIN
 PERFORM 1 FROM public.scheduler_control c WHERE c.control_id='deployment' FOR SHARE;
 SELECT c.last_transaction INTO previous FROM public.scheduler_control c WHERE c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation FOR UPDATE;
 IF NOT FOUND OR previous=pg_current_xact_id() THEN
  RAISE EXCEPTION 'one operation per candidate transaction required' USING ERRCODE='25000';
 END IF;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.lock_control(uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.lock_control(uuid) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.claim(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_uid uuid,p_membership uuid,p_v bigint,p_g bigint,p_epoch bigint,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE s public.monitor_schedules%ROWTYPE; provisional timestamptz; gate_now timestamptz; new_fence bigint;
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 IF p_uid IS NULL OR p_membership IS NULL OR p_run IS NULL THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,p_uid,p_membership,true);
 SELECT * INTO s FROM public.monitor_schedules x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid;
 IF s.next_run_id IS DISTINCT FROM p_run OR s.scheduler_epoch IS DISTINCT FROM p_epoch THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.monitor_runs r WHERE r.workspace_id=p_wid AND r.project_id=p_pid AND r.monitor_id=p_mid
 AND (r.attempt_state IN ('leased','started') OR r.physical_release_confirmed_at IS NULL) FOR UPDATE;
 IF FOUND THEN RAISE EXCEPTION 'candidate_busy' USING ERRCODE='P0001'; END IF;
 new_fence:=s.last_fencing_token+1;
 provisional:=GREATEST(clock_timestamp(),s.next_due_at);
 INSERT INTO public.monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state)
 VALUES(p_wid,p_pid,p_mid,p_run,p_v,p_g,p_epoch,new_fence,COALESCE(s.next_due_at,provisional),provisional,session_user,p_incarnation,provisional+interval '45 seconds','leased');
 PERFORM scheduler_private.lock_budgets(p_wid,p_pid,p_mid);
 INSERT INTO public.scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state) VALUES(p_wid,p_pid,p_mid,p_run,'reserved');
 PERFORM scheduler_private.lock_control(p_incarnation);
 -- Separate SPI command and fresh snapshot after every lock, including control.
 SELECT clock_timestamp() INTO gate_now;
 PERFORM 1 FROM public.users u JOIN public.workspace_members b ON b.user_id=u.id AND b.workspace_id=p_wid
 JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 JOIN public.monitor_schedules x ON x.workspace_id=m.workspace_id AND x.project_id=m.project_id AND x.monitor_id=m.id
 CROSS JOIN public.scheduler_control c
 WHERE u.id=p_uid AND u.status='active' AND b.role='owner' AND b.membership_id=p_membership
 AND m.version=p_v AND a.desired_state='enabled' AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND x.authorized_configuration_version=p_v AND x.authorized_activation_generation=p_g AND x.scheduler_epoch=p_epoch AND x.consent_protocol=1
 AND x.expires_at>gate_now AND (x.next_due_at IS NULL OR x.next_due_at<=gate_now) AND (x.next_start_not_before IS NULL OR x.next_start_not_before<=gate_now)
 AND c.control_id='deployment' AND c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now
 AND c.disk_used_bytes<c.disk_budget_bytes*0.8
 AND NOT EXISTS(SELECT 1 FROM public.scheduler_control h WHERE h.kind='http' AND h.routable AND
 (h.projection_protocol IS DISTINCT FROM 'intent-v2' OR NOT h.status_ready OR h.expires_at<=gate_now OR h.expires_at IS NULL));
 IF NOT FOUND OR gate_now>transaction_timestamp()+interval '2 seconds' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 UPDATE public.scheduler_budget_state SET in_use=in_use+1,active_count=active_count+1,terminal_count=terminal_count+1,keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE kind='global' AND budget_key='global' AND in_use<32 AND active_count<32 AND terminal_count<100000;
 IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1,terminal_count=terminal_count+1,keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE kind='workspace' AND budget_key=p_wid::text AND in_use<4 AND terminal_count<10000;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1,keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE kind='project' AND budget_key=p_wid::text||'/'||p_pid::text AND in_use<2;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1,terminal_count=terminal_count+1,keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND in_use<1 AND terminal_count<100;
 IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
 UPDATE public.monitor_runs SET claimed_at=gate_now,lease_until=gate_now+interval '45 seconds' WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 UPDATE public.monitor_schedules SET last_fencing_token=new_fence,last_attempt_run_id=p_run,next_due_at=NULL,blocked_reason=NULL,updated_at=GREATEST(updated_at,gate_now)
 WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=gate_now WHERE kind='worker' AND login_name=session_user AND incarnation=p_incarnation;
 RETURN jsonb_build_object('workspace_id',p_wid,'project_id',p_pid,'monitor_id',p_mid,'run_id',p_run,'configuration_version',p_v::text,
 'activation_generation',p_g::text,'scheduler_epoch',p_epoch::text,'fencing_token',new_fence::text,'claimed_at',gate_now,'lease_until',gate_now+interval '45 seconds');
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.claim(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.claim(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.claim(uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,uuid) TO devpilot_scheduler_worker;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.candidates(p_incarnation uuid,p_after uuid) RETURNS jsonb
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE result jsonb;
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 -- No row locks: every returned fact is a hint and is revalidated at Claim.
 WITH candidate_workspaces AS MATERIALIZED (
  SELECT s.workspace_id FROM public.monitor_schedules s
  WHERE s.authorized_configuration_version IS NOT NULL AND s.expires_at>clock_timestamp()
  AND (s.next_due_at IS NULL OR s.next_due_at<=clock_timestamp())
  AND (s.next_start_not_before IS NULL OR s.next_start_not_before<=clock_timestamp())
  AND EXISTS(SELECT 1 FROM public.workspace_members b JOIN public.users u ON u.id=b.user_id WHERE b.workspace_id=s.workspace_id AND b.role='owner' AND u.status='active')
  AND NOT EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=s.workspace_id AND r.project_id=s.project_id AND r.monitor_id=s.monitor_id AND (r.attempt_state IN ('leased','started') OR r.physical_release_confirmed_at IS NULL))
  GROUP BY s.workspace_id ORDER BY CASE WHEN p_after IS NULL OR s.workspace_id>p_after THEN 0 ELSE 1 END,s.workspace_id LIMIT 64
 ), hints AS (
  SELECT x.*,o.user_id,o.membership_id FROM candidate_workspaces w
  CROSS JOIN LATERAL (
   SELECT s.workspace_id,s.project_id,s.monitor_id,s.authorized_configuration_version v,s.authorized_activation_generation g,s.scheduler_epoch,s.next_run_id,s.next_due_at
   FROM public.monitor_schedules s WHERE s.workspace_id=w.workspace_id AND s.authorized_configuration_version IS NOT NULL AND s.expires_at>clock_timestamp()
   AND (s.next_due_at IS NULL OR s.next_due_at<=clock_timestamp()) AND (s.next_start_not_before IS NULL OR s.next_start_not_before<=clock_timestamp())
   AND NOT EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=s.workspace_id AND r.project_id=s.project_id AND r.monitor_id=s.monitor_id AND (r.attempt_state IN ('leased','started') OR r.physical_release_confirmed_at IS NULL))
   ORDER BY s.next_due_at NULLS FIRST,s.project_id,s.monitor_id LIMIT 8
  ) x CROSS JOIN LATERAL (
   SELECT b.user_id,b.membership_id FROM public.workspace_members b JOIN public.users u ON u.id=b.user_id
   WHERE b.workspace_id=w.workspace_id AND b.role='owner' AND u.status='active' ORDER BY b.user_id LIMIT 1
  ) o
 ) SELECT COALESCE(jsonb_agg(jsonb_build_object('workspace_id',h.workspace_id,'project_id',h.project_id,'monitor_id',h.monitor_id,
 'owner_user_id',h.user_id,'membership_id',h.membership_id,'configuration_version',h.v::text,'activation_generation',h.g::text,
 'scheduler_epoch',h.scheduler_epoch::text,'run_id',h.next_run_id)
 ORDER BY CASE WHEN p_after IS NULL OR h.workspace_id>p_after THEN 0 ELSE 1 END,h.workspace_id,h.next_due_at NULLS FIRST,h.project_id,h.monitor_id),'[]')
 INTO result FROM hints h;
 RETURN result;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.candidates(uuid,uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.candidates(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.candidates(uuid,uuid) TO devpilot_scheduler_worker;

ALTER TABLE public.scheduler_reservations ADD COLUMN prepared_at timestamptz;
ALTER TABLE public.scheduler_reservations ADD COLUMN preparation_digest bytea CHECK(octet_length(preparation_digest)=32);
ALTER TABLE public.scheduler_reservations ADD COLUMN input_consumed_at timestamptz;
ALTER TABLE public.scheduler_reservations ADD COLUMN target_permit_id uuid;
ALTER TABLE public.scheduler_reservations ADD COLUMN target_permit_committed_at timestamptz;
ALTER TABLE public.scheduler_reservations ADD COLUMN permit_installed_at timestamptz;
ALTER TABLE public.scheduler_reservations ADD CONSTRAINT scheduler_preparation_shape CHECK(
 (namespace_incarnation IS NULL AND prepared_at IS NULL AND preparation_digest IS NULL AND permit_deadline IS NULL)
 OR(namespace_incarnation IS NOT NULL AND prepared_at IS NOT NULL AND preparation_digest IS NOT NULL AND permit_deadline>prepared_at AND permit_deadline<=prepared_at+interval '35 seconds'));
ALTER TABLE public.scheduler_reservations ADD CONSTRAINT scheduler_target_permit_shape CHECK(
 (origin_key IS NULL AND ip_key IS NULL AND target_permit_id IS NULL AND target_permit_committed_at IS NULL)
 OR(origin_key IS NOT NULL AND ip_key IS NOT NULL AND target_permit_id IS NOT NULL AND target_permit_committed_at IS NOT NULL));

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.reservation_guard() RETURNS trigger LANGUAGE plpgsql SET search_path=pg_catalog,scheduler_private AS $$
DECLARE administrator boolean; supervisor boolean;
BEGIN
 SELECT r.rolsuper INTO administrator FROM pg_catalog.pg_roles r WHERE r.rolname=session_user;
 supervisor:=administrator OR pg_has_role(session_user,'devpilot_scheduler_supervisor','MEMBER');
 IF ROW(NEW.workspace_id,NEW.project_id,NEW.monitor_id,NEW.run_id,NEW.reservation_id)
 IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.monitor_id,OLD.run_id,OLD.reservation_id)
 OR (OLD.namespace_incarnation IS NOT NULL AND ROW(NEW.namespace_incarnation,NEW.supervisor_incarnation,NEW.prepared_at,NEW.preparation_digest,NEW.permit_deadline)
 IS DISTINCT FROM ROW(OLD.namespace_incarnation,OLD.supervisor_incarnation,OLD.prepared_at,OLD.preparation_digest,OLD.permit_deadline))
 OR (OLD.origin_key IS NOT NULL AND ROW(NEW.origin_key,NEW.ip_key,NEW.target_permit_id,NEW.target_permit_committed_at) IS DISTINCT FROM ROW(OLD.origin_key,OLD.ip_key,OLD.target_permit_id,OLD.target_permit_committed_at))
 OR (OLD.input_consumed_at IS NOT NULL AND NEW.input_consumed_at IS DISTINCT FROM OLD.input_consumed_at)
 OR (OLD.permit_installed_at IS NOT NULL AND NEW.permit_installed_at IS DISTINCT FROM OLD.permit_installed_at)
 OR (OLD.reservation_state='released' AND NEW IS DISTINCT FROM OLD) THEN
  RAISE EXCEPTION 'reservation lineage or consumed capability is immutable' USING ERRCODE='23514';
 END IF;
 IF NOT supervisor AND (ROW(NEW.namespace_incarnation,NEW.supervisor_incarnation,NEW.prepared_at,NEW.preparation_digest,NEW.permit_deadline,NEW.input_consumed_at,NEW.permit_installed_at,NEW.released_at,NEW.termination_receipt)
 IS DISTINCT FROM ROW(OLD.namespace_incarnation,OLD.supervisor_incarnation,OLD.prepared_at,OLD.preparation_digest,OLD.permit_deadline,OLD.input_consumed_at,OLD.permit_installed_at,OLD.released_at,OLD.termination_receipt)
 OR NEW.reservation_state='released') THEN
  RAISE EXCEPTION 'independent supervisor required' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER scheduler_reservation_guard BEFORE UPDATE ON public.scheduler_reservations FOR EACH ROW EXECUTE FUNCTION scheduler_private.reservation_guard();
REVOKE ALL ON FUNCTION scheduler_private.reservation_guard() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.consume_token(p_kind text,p_key text,p_capacity bigint,p_rate bigint,p_now timestamptz) RETURNS void
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
DECLARE b public.scheduler_budget_state%ROWTYPE; available bigint;
BEGIN
 SELECT * INTO STRICT b FROM public.scheduler_budget_state x WHERE x.kind=p_kind AND x.budget_key=p_key;
 IF p_now<b.last_refill_at THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 available:=floor(LEAST(p_capacity::numeric,b.tokens::numeric+GREATEST(0,extract(epoch FROM p_now-b.last_refill_at))*p_rate))::bigint;
 IF available<1000000 THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET tokens=available-1000000,last_refill_at=p_now,keep_until=GREATEST(keep_until,p_now+interval '1 hour') WHERE kind=p_kind AND budget_key=p_key;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.consume_token(text,text,bigint,bigint,timestamptz) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.consume_token(text,text,bigint,bigint,timestamptz) FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.start_run(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_uid uuid,p_membership uuid,p_run uuid,p_fence bigint)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE; gate_now timestamptz; configuration public.http_monitors%ROWTYPE;
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 IF p_uid IS NULL OR p_membership IS NULL THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,p_uid,p_membership,false);
 SELECT * INTO r FROM public.monitor_runs x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND OR r.lease_owner<>session_user OR r.worker_incarnation<>p_incarnation OR r.fencing_token<>p_fence OR r.attempt_state<>'leased' THEN
  RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_budgets(p_wid,p_pid,p_mid);
 SELECT * INTO q FROM public.scheduler_reservations x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_control(p_incarnation);
 -- The registered supervisor is also protected last, never before a parent.
 PERFORM 1 FROM public.scheduler_control c WHERE c.kind='supervisor' AND c.incarnation=q.supervisor_incarnation FOR SHARE;
 SELECT clock_timestamp() INTO gate_now;
 SELECT m.* INTO configuration FROM public.users u JOIN public.workspace_members b ON b.user_id=u.id AND b.workspace_id=p_wid
 JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 JOIN public.monitor_schedules s ON s.workspace_id=m.workspace_id AND s.project_id=m.project_id AND s.monitor_id=m.id
 CROSS JOIN public.scheduler_control c
 WHERE u.id=p_uid AND u.status='active' AND b.role='owner' AND b.membership_id=p_membership
 AND m.version=r.configuration_version AND a.desired_state='enabled' AND a.activation_generation=r.activation_generation AND a.activation_configuration_version=m.version
 AND s.authorized_configuration_version=m.version AND s.authorized_activation_generation=a.activation_generation AND s.scheduler_epoch=r.scheduler_epoch AND s.consent_protocol=1
 AND s.last_fencing_token=p_fence AND s.expires_at>gate_now
 AND (s.next_start_not_before IS NULL OR s.next_start_not_before<=gate_now)
 AND r.lease_until>=gate_now+(m.timeout_seconds+5)*interval '1 second'
 AND q.reservation_state='reserved' AND q.namespace_incarnation IS NOT NULL AND q.input_consumed_at IS NULL
 AND q.permit_deadline>=gate_now+m.timeout_seconds*interval '1 second' AND q.permit_deadline<=gate_now+interval '35 seconds'
 AND EXISTS(SELECT 1 FROM public.scheduler_control z WHERE z.kind='supervisor' AND z.incarnation=q.supervisor_incarnation AND z.verified_at IS NOT NULL AND NOT z.emergency_stop AND z.expires_at>gate_now)
 AND c.control_id='deployment' AND c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now AND c.disk_used_bytes<c.disk_budget_bytes*0.8
 AND NOT EXISTS(SELECT 1 FROM public.scheduler_control h WHERE h.kind='http' AND h.routable AND
 (h.projection_protocol IS DISTINCT FROM 'intent-v2' OR NOT h.status_ready OR h.expires_at<=gate_now OR h.expires_at IS NULL));
 IF NOT FOUND OR gate_now>transaction_timestamp()+interval '2 seconds' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 PERFORM scheduler_private.consume_token('global','global',8000000,8000000,gate_now);
 PERFORM scheduler_private.consume_token('workspace',p_wid::text,2000000,1000000,gate_now);
 PERFORM scheduler_private.consume_token('project',p_wid::text||'/'||p_pid::text,1000000,500000,gate_now);
 UPDATE public.scheduler_budget_state SET not_before=GREATEST(not_before,gate_now+configuration.interval_seconds*interval '1 second'),keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text;
 UPDATE public.monitor_runs SET attempt_state='started',started_at=gate_now WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 UPDATE public.monitor_schedules SET last_started_at=gate_now,next_start_not_before=GREATEST(next_start_not_before,gate_now+configuration.interval_seconds*interval '1 second'),updated_at=GREATEST(updated_at,gate_now)
 WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=gate_now WHERE kind='worker' AND login_name=session_user AND incarnation=p_incarnation;
 -- URL exists only in this transient Start snapshot, never in a run or log.
 RETURN jsonb_build_object('workspace_id',p_wid,'project_id',p_pid,'monitor_id',p_mid,'run_id',p_run,
 'configuration_version',r.configuration_version::text,'activation_generation',r.activation_generation::text,'scheduler_epoch',r.scheduler_epoch::text,'fencing_token',p_fence::text,
 'started_at',gate_now,'permit_deadline',q.permit_deadline,'url',configuration.url,'timeout_seconds',configuration.timeout_seconds,'expected_status',configuration.expected_status,'interval_seconds',configuration.interval_seconds,
 'namespace_incarnation',q.namespace_incarnation,'supervisor_incarnation',q.supervisor_incarnation);
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.start_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.start_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.start_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint) TO devpilot_scheduler_worker;

-- Supervisor-only physical capability records. These functions authenticate the
-- independent supervisor; they do not themselves inspect Linux. The trusted
-- supervisor must verify the real namespace/process/rules before calling them.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.prepare_run(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_run uuid,p_worker_incarnation uuid,p_namespace uuid,p_remaining_ms integer,p_manifest bytea)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE; gate_now timestamptz; deadline timestamptz; previous xid8; timeout_seconds integer;
BEGIN
 PERFORM scheduler_private.system_identity('supervisor',p_incarnation);
 IF p_namespace IS NULL OR p_remaining_ms IS NULL OR p_remaining_ms<1 OR p_remaining_ms>35000 OR p_manifest IS NULL OR octet_length(p_manifest)<>32 THEN RAISE EXCEPTION 'invalid preparation' USING ERRCODE='22023'; END IF;
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,NULL,NULL,false);
 SELECT * INTO r FROM public.monitor_runs x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND OR r.worker_incarnation IS DISTINCT FROM p_worker_incarnation OR r.attempt_state<>'leased' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_budgets(p_wid,p_pid,p_mid);
 SELECT * INTO q FROM public.scheduler_reservations x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND OR q.namespace_incarnation IS NOT NULL OR q.reservation_state<>'reserved' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.scheduler_control c WHERE c.control_id='deployment' FOR SHARE;
 SELECT c.last_transaction INTO previous FROM public.scheduler_control c WHERE c.kind='supervisor' AND c.login_name=session_user AND c.incarnation=p_incarnation FOR UPDATE;
 IF NOT FOUND OR previous=pg_current_xact_id() THEN RAISE EXCEPTION 'one operation per transaction required' USING ERRCODE='25000'; END IF;
 SELECT clock_timestamp() INTO gate_now;
 PERFORM scheduler_private.system_identity('supervisor',p_incarnation);
 SELECT m.timeout_seconds INTO timeout_seconds FROM public.http_monitors m WHERE m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid;
 deadline:=gate_now+p_remaining_ms*interval '1 millisecond';
 IF r.lease_until<=gate_now OR deadline<gate_now+timeout_seconds*interval '1 second'
 OR NOT EXISTS(SELECT 1 FROM public.scheduler_control c WHERE c.control_id='deployment' AND c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now) THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_reservations SET namespace_incarnation=p_namespace,supervisor_incarnation=p_incarnation,prepared_at=gate_now,permit_deadline=deadline,preparation_digest=p_manifest WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=gate_now WHERE kind='supervisor' AND login_name=session_user AND incarnation=p_incarnation;
 RETURN jsonb_build_object('namespace_incarnation',p_namespace,'supervisor_incarnation',p_incarnation,'permit_deadline',deadline);
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.prepare_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,integer,bytea) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.prepare_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,integer,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.prepare_run(uuid,uuid,uuid,uuid,uuid,uuid,uuid,integer,bytea) TO devpilot_scheduler_supervisor;

-- Consumption happens BEFORE pipe delivery. A commit ACK loss or partial pipe
-- write never restores the single input. No execution snapshot is persisted.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.consume_input(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_run uuid,p_namespace uuid,p_fence bigint)
RETURNS void LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE; gate_now timestamptz; previous xid8;
BEGIN
 PERFORM scheduler_private.system_identity('supervisor',p_incarnation);
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,NULL,NULL,false);
 SELECT * INTO r FROM public.monitor_runs x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_budgets(p_wid,p_pid,p_mid);
 SELECT * INTO q FROM public.scheduler_reservations x WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.scheduler_control c WHERE c.control_id='deployment' FOR SHARE;
 SELECT c.last_transaction INTO previous FROM public.scheduler_control c WHERE c.kind='supervisor' AND c.login_name=session_user AND c.incarnation=p_incarnation FOR UPDATE;
 IF NOT FOUND OR previous=pg_current_xact_id() THEN RAISE EXCEPTION 'one operation per transaction required' USING ERRCODE='25000'; END IF;
 SELECT clock_timestamp() INTO gate_now;
 PERFORM scheduler_private.system_identity('supervisor',p_incarnation);
 IF r.attempt_state<>'started' OR r.fencing_token IS DISTINCT FROM p_fence OR r.lease_until<=gate_now
 OR q.reservation_state<>'reserved' OR q.supervisor_incarnation IS DISTINCT FROM p_incarnation OR q.namespace_incarnation IS DISTINCT FROM p_namespace
 OR q.input_consumed_at IS NOT NULL OR q.permit_deadline<=gate_now
 OR NOT EXISTS(SELECT 1 FROM public.scheduler_control c WHERE c.control_id='deployment' AND c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now) THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_reservations SET input_consumed_at=gate_now WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=gate_now WHERE kind='supervisor' AND login_name=session_user AND incarnation=p_incarnation;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.consume_input(uuid,uuid,uuid,uuid,uuid,uuid,bigint) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.consume_input(uuid,uuid,uuid,uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.consume_input(uuid,uuid,uuid,uuid,uuid,uuid,bigint) TO devpilot_scheduler_supervisor;

-- +goose Down
LOCK TABLE public.http_monitors IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE public.monitor_activation_states IN ACCESS EXCLUSIVE MODE;
-- A disposable down is not an operational rollback. Even here, refuse to drop
-- the only physical-permission ledger until independent termination is proven.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.monitor_runs WHERE physical_release_confirmed_at IS NULL)
 OR EXISTS(SELECT 1 FROM public.scheduler_control WHERE gate<>'closed') THEN
  RAISE EXCEPTION 'independent physical stop and closed gate required';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER scheduler_activation_revoke ON public.monitor_activation_states;
DROP SCHEMA scheduler_private CASCADE;
ALTER TABLE public.monitor_schedules DROP CONSTRAINT monitor_schedules_last_attempt_fk;
ALTER TABLE public.monitor_schedules DROP CONSTRAINT monitor_schedules_latest_result_fk;
DROP TABLE public.scheduler_reservations,public.monitor_runs,public.monitor_schedules,public.scheduler_budget_state,public.scheduler_control;
-- Retire ALL tags before restoring the schema7 representation. Never revive
-- enabled or shorten cooldown. Overflow rolls down back rather than reusing tags.
UPDATE public.monitor_activation_states SET desired_state='disabled',activation_configuration_version=NULL,
 activation_generation=activation_generation+1,last_transition_kind='configuration_changed',last_actor_user_id=NULL,
 updated_at=GREATEST(updated_at,statement_timestamp());
ALTER TABLE public.monitor_activation_states DROP CONSTRAINT monitor_activation_states_check1;
ALTER TABLE public.monitor_activation_states ADD CONSTRAINT monitor_activation_states_check1 CHECK (
 (desired_state='enabled' AND activation_configuration_version IS NOT NULL AND next_enable_allowed_at IS NOT NULL
  AND last_actor_user_id IS NOT NULL AND last_transition_kind='owner_enabled')
 OR (desired_state='disabled' AND activation_configuration_version IS NULL AND (
  (last_transition_kind='owner_paused' AND last_actor_user_id IS NOT NULL)
  OR (last_transition_kind IN ('never_activated','configuration_changed') AND last_actor_user_id IS NULL)))
);
DROP OWNED BY devpilot_scheduler_api_owner,devpilot_scheduler_owner,devpilot_scheduler_controller,devpilot_scheduler_worker,devpilot_scheduler_supervisor,devpilot_scheduler_api;
DROP ROLE devpilot_scheduler_api_owner,devpilot_scheduler_api,devpilot_scheduler_supervisor,devpilot_scheduler_worker,devpilot_scheduler_controller,devpilot_scheduler_owner;
