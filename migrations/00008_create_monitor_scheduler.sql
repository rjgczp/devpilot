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
   blocked_reason='authorization_required',updated_at=GREATEST(updated_at,clock_timestamp())
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
 IF ROW(NEW.workspace_id,NEW.project_id,NEW.monitor_id,NEW.run_id,NEW.configuration_version,NEW.activation_generation,NEW.scheduler_epoch,NEW.fencing_token,NEW.scheduled_at,NEW.claimed_at,NEW.lease_owner,NEW.worker_incarnation)
 IS DISTINCT FROM ROW(OLD.workspace_id,OLD.project_id,OLD.monitor_id,OLD.run_id,OLD.configuration_version,OLD.activation_generation,OLD.scheduler_epoch,OLD.fencing_token,OLD.scheduled_at,OLD.claimed_at,OLD.lease_owner,OLD.worker_incarnation)
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
CREATE TRIGGER scheduler_run_guard BEFORE UPDATE ON public.monitor_runs FOR EACH ROW EXECUTE FUNCTION scheduler_private.run_guard();

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
  scheduler_epoch=public.monitor_schedules.scheduler_epoch+1,next_due_at=CASE WHEN permitted THEN GREATEST(gate_now,public.monitor_schedules.next_start_not_before) END,
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
 'authorization_state',authorization_state,'scheduling_state',CASE WHEN desired_state='disabled' THEN 'disabled' WHEN reason IS NOT NULL THEN 'blocked' WHEN next_due_at<=now THEN 'due' ELSE 'authorized_waiting' END,
 'effective_authorization',desired_state='enabled' AND authorization_state='valid' AND reason IS NULL,
 'expires_at',expires_at,'next_due',next_due_at,'in_flight',in_flight,'pause_semantics','start_commit','blocked_reason',reason,
 'latest_attempt',attempt,'latest_completed_observation',observation,
 'current_observation',CASE WHEN attempt_state IS DISTINCT FROM 'unknown' AND completed_v=version AND completed_g=activation_generation AND completed_e=scheduler_epoch THEN observation END) FROM states);
$$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) OWNER TO devpilot_scheduler_api_owner;
REVOKE ALL ON FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.execution_status(bytea,uuid,uuid,uuid) TO devpilot_scheduler_api;

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
