-- +goose Up
-- Task8.1 only. No cleanup, physical attestation, network authority or new runtime EXECUTE.
-- Maintenance DDL follows the parent suborder; operational writers also start at parents.
LOCK TABLE public.workspaces,public.workspace_members,public.projects,public.http_monitors,
 public.monitor_activation_states,public.monitor_schedules,public.monitor_runs,
 public.scheduler_budget_state,public.scheduler_reservations,public.scheduler_control IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND (gate<>'closed' OR NOT emergency_stop)) THEN
  RAISE EXCEPTION 'capacity upgrade requires closed fixture gate' USING ERRCODE='55000';
 END IF;
 -- Refuse to repair an unexplained ledger difference by overwriting it.
 IF NOT EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global')
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE
  (b.kind='global' AND (b.terminal_count<>(SELECT count(*) FROM public.monitor_runs)
   OR b.schedule_count<>(SELECT count(*) FROM public.monitor_schedules)
   OR b.key_count<>(SELECT count(*) FROM public.scheduler_budget_state))))
 OR EXISTS(WITH actual AS (
  SELECT 'workspace'::text kind,r.workspace_id::text budget_key,count(*) n FROM public.monitor_runs r GROUP BY r.workspace_id
  UNION ALL SELECT 'monitor',r.workspace_id::text||'/'||r.project_id::text||'/'||r.monitor_id::text,count(*)
   FROM public.monitor_runs r GROUP BY r.workspace_id,r.project_id,r.monitor_id
 ) SELECT 1 FROM actual a FULL JOIN (SELECT * FROM public.scheduler_budget_state WHERE kind IN ('workspace','monitor')) b USING(kind,budget_key)
 WHERE b.budget_key IS NULL OR COALESCE(a.n,0)<>b.terminal_count) THEN
  RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514';
 END IF;
 IF (SELECT count(*) FROM public.monitor_runs WHERE attempt_state IN ('leased','started') OR physical_release_confirmed_at IS NULL)>32 THEN
  RAISE EXCEPTION 'retention_capacity' USING ERRCODE='23514';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE public.scheduler_budget_state ADD COLUMN retention_pending_count integer NOT NULL DEFAULT 0;
ALTER TABLE public.scheduler_budget_state ADD CONSTRAINT scheduler_retention_pending_cap
 CHECK(retention_pending_count BETWEEN 0 AND 32 AND (kind='global' OR retention_pending_count=0));
UPDATE public.scheduler_budget_state SET retention_pending_count=(SELECT count(*) FROM public.monitor_runs
 WHERE attempt_state IN ('leased','started') OR physical_release_confirmed_at IS NULL) WHERE kind='global' AND budget_key='global';
-- Keep all old-column UPDATE capabilities, but do not inherit write access to the new column.
REVOKE UPDATE ON public.scheduler_budget_state FROM devpilot_scheduler_api_owner;
GRANT UPDATE(kind,budget_key,in_use,tokens,last_refill_at,not_before,keep_until,
 terminal_count,active_count,schedule_count,key_count) ON public.scheduler_budget_state TO devpilot_scheduler_api_owner;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.run_capacity_accounting() RETURNS trigger
LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE w uuid; p uuid; m uuid; old_pending boolean; new_pending boolean;
BEGIN
 IF TG_OP='TRUNCATE' THEN RAISE EXCEPTION 'capacity ledger cannot be truncated' USING ERRCODE='23514'; END IF;
 -- Callers already hold parents / existing Run / G,W,P,M before Reservation.
 -- Never initialize a budget or acquire another parent/Run/Reservation here.
 IF TG_OP='UPDATE' THEN
  old_pending:=OLD.attempt_state IN ('leased','started') OR OLD.physical_release_confirmed_at IS NULL;
  new_pending:=NEW.attempt_state IN ('leased','started') OR NEW.physical_release_confirmed_at IS NULL;
  IF old_pending=new_pending THEN RETURN NEW; END IF;
  -- A Run can never restart or lose its independent termination fact.
  IF new_pending THEN RAISE EXCEPTION 'terminal capacity cannot restart' USING ERRCODE='23514'; END IF;
  UPDATE public.scheduler_budget_state SET retention_pending_count=retention_pending_count-1
  WHERE kind='global' AND budget_key='global' AND retention_pending_count>0;
  IF NOT FOUND THEN RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='INSERT' THEN
  w:=NEW.workspace_id;p:=NEW.project_id;m:=NEW.monitor_id;
  new_pending:=NEW.attempt_state IN ('leased','started') OR NEW.physical_release_confirmed_at IS NULL;
  IF EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=w AND r.project_id=p AND r.monitor_id=m
   AND r.finished_at<=clock_timestamp()-interval '7 days') THEN
   RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
  IF NOT EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='project' AND budget_key=w::text||'/'||p::text) THEN
   RAISE EXCEPTION 'capacity ledger missing budget' USING ERRCODE='23514'; END IF;
  UPDATE public.scheduler_budget_state SET terminal_count=terminal_count+1,
   retention_pending_count=retention_pending_count+CASE WHEN new_pending THEN 1 ELSE 0 END
  WHERE kind='global' AND budget_key='global' AND terminal_count<100000
   AND retention_pending_count+CASE WHEN new_pending THEN 1 ELSE 0 END<=32;
  IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
  UPDATE public.scheduler_budget_state SET terminal_count=terminal_count+1
  WHERE kind='workspace' AND budget_key=w::text AND terminal_count<10000;
  IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
  UPDATE public.scheduler_budget_state SET terminal_count=terminal_count+1
  WHERE kind='monitor' AND budget_key=w::text||'/'||p::text||'/'||m::text AND terminal_count<100;
  IF NOT FOUND THEN RAISE EXCEPTION 'retention_capacity' USING ERRCODE='P0001'; END IF;
  RETURN NEW;
 END IF;
 w:=OLD.workspace_id;p:=OLD.project_id;m:=OLD.monitor_id;
 IF OLD.attempt_state IN ('leased','started') OR OLD.physical_release_confirmed_at IS NULL
 OR EXISTS(SELECT 1 FROM public.scheduler_reservations q WHERE q.workspace_id=w AND q.project_id=p AND q.monitor_id=m AND q.run_id=OLD.run_id)
 OR EXISTS(SELECT 1 FROM public.monitor_schedules s WHERE s.workspace_id=w AND s.project_id=p AND s.monitor_id=m
  AND (s.last_attempt_run_id=OLD.run_id OR s.latest_result_run_id=OLD.run_id OR s.expires_at>clock_timestamp()
   OR s.next_start_not_before>clock_timestamp() OR s.last_started_at>clock_timestamp()-interval '1 hour'))
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE b.kind='monitor' AND b.budget_key=w::text||'/'||p::text||'/'||m::text
  AND (b.keep_until>clock_timestamp() OR b.not_before>clock_timestamp())) THEN
  RAISE EXCEPTION 'run still owns physical permission reference or cooling' USING ERRCODE='23514'; END IF;
 UPDATE public.scheduler_budget_state SET terminal_count=terminal_count-1 WHERE kind='global' AND budget_key='global' AND terminal_count>0;
 IF NOT FOUND THEN RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514'; END IF;
 UPDATE public.scheduler_budget_state SET terminal_count=terminal_count-1 WHERE kind='workspace' AND budget_key=w::text AND terminal_count>0;
 IF NOT FOUND THEN RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514'; END IF;
 UPDATE public.scheduler_budget_state SET terminal_count=terminal_count-1 WHERE kind='monitor' AND budget_key=w::text||'/'||p::text||'/'||m::text AND terminal_count>0;
 IF NOT FOUND THEN RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514'; END IF;
 RETURN OLD;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.run_capacity_accounting() OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.run_capacity_accounting() FROM PUBLIC;
CREATE TRIGGER scheduler_run_capacity_insert AFTER INSERT ON public.monitor_runs FOR EACH ROW EXECUTE FUNCTION scheduler_private.run_capacity_accounting();
CREATE TRIGGER scheduler_run_capacity_update AFTER UPDATE ON public.monitor_runs FOR EACH ROW EXECUTE FUNCTION scheduler_private.run_capacity_accounting();
CREATE TRIGGER scheduler_run_capacity_delete BEFORE DELETE ON public.monitor_runs FOR EACH ROW EXECUTE FUNCTION scheduler_private.run_capacity_accounting();
CREATE TRIGGER scheduler_run_capacity_truncate BEFORE TRUNCATE ON public.monitor_runs FOR EACH STATEMENT EXECUTE FUNCTION scheduler_private.run_capacity_accounting();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.claim(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_uid uuid,p_membership uuid,p_v bigint,p_g bigint,p_epoch bigint,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE s public.monitor_schedules%ROWTYPE; provisional timestamptz; gate_now timestamptz; observed_now timestamptz; previous_gate timestamptz; new_fence bigint;
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
 -- Initialize and lock all budget rows BEFORE the first Run INSERT.
 PERFORM scheduler_private.lock_budgets(p_wid,p_pid,p_mid);
 INSERT INTO public.monitor_runs(workspace_id,project_id,monitor_id,run_id,configuration_version,activation_generation,scheduler_epoch,fencing_token,scheduled_at,claimed_at,lease_owner,worker_incarnation,lease_until,attempt_state)
 VALUES(p_wid,p_pid,p_mid,p_run,p_v,p_g,p_epoch,new_fence,COALESCE(s.next_due_at,provisional),provisional,session_user,p_incarnation,provisional+interval '45 seconds','leased');
 UPDATE public.scheduler_budget_state SET in_use=in_use+1,active_count=active_count+1
 WHERE kind='global' AND budget_key='global' AND in_use<32 AND active_count<32;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1
 WHERE kind='workspace' AND budget_key=p_wid::text AND in_use<4;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1
 WHERE kind='project' AND budget_key=p_wid::text||'/'||p_pid::text AND in_use<2;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 UPDATE public.scheduler_budget_state SET in_use=in_use+1
 WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND in_use<1;
 IF NOT FOUND THEN RAISE EXCEPTION 'resource_limited' USING ERRCODE='P0001'; END IF;
 INSERT INTO public.scheduler_reservations(workspace_id,project_id,monitor_id,run_id,reservation_state) VALUES(p_wid,p_pid,p_mid,p_run,'reserved');
 PERFORM scheduler_private.lock_control(p_incarnation);
 -- Separate SPI command and fresh snapshot after every lock, including control.
 SELECT clock_timestamp(),c.last_gate_at INTO observed_now,previous_gate FROM public.scheduler_control c
 WHERE c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation;
 gate_now:=GREATEST(observed_now,s.updated_at,previous_gate);
 -- A backwards step exceeding the existing2s transaction budget cannot be
 -- absorbed into this operation; refuse rather than moving durable time back.
 IF gate_now>observed_now+interval '2 seconds' THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.users u JOIN public.workspace_members b ON b.user_id=u.id AND b.workspace_id=p_wid
 JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 JOIN public.monitor_schedules x ON x.workspace_id=m.workspace_id AND x.project_id=m.project_id AND x.monitor_id=m.id
 CROSS JOIN public.scheduler_control c
 WHERE u.id=p_uid AND u.status='active' AND b.role='owner' AND b.membership_id=p_membership
 AND m.version=p_v AND a.desired_state='enabled' AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND x.authorized_configuration_version=p_v AND x.authorized_activation_generation=p_g AND x.scheduler_epoch=p_epoch AND x.consent_protocol=1
 AND (x.last_started_at IS NULL OR x.last_started_at+m.interval_seconds*interval '1 second'<=gate_now)
 AND x.expires_at>gate_now AND (x.next_due_at IS NULL OR x.next_due_at<=gate_now) AND (x.next_start_not_before IS NULL OR x.next_start_not_before<=gate_now)
 AND c.control_id='deployment' AND c.gate='fixture' AND NOT c.emergency_stop AND c.expires_at>gate_now
 AND c.disk_used_bytes<c.disk_budget_bytes*0.8
 AND NOT EXISTS(SELECT 1 FROM public.scheduler_control h WHERE h.kind='http' AND h.routable AND
 (h.projection_protocol IS DISTINCT FROM 'intent-v2' OR NOT h.status_ready OR h.expires_at<=gate_now OR h.expires_at IS NULL));
 IF NOT FOUND OR gate_now>transaction_timestamp()+interval '2 seconds' THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 -- All capacity was reserved before control locks; only finalize its time floor now.
 UPDATE public.scheduler_budget_state SET keep_until=GREATEST(keep_until,gate_now+interval '1 hour')
 WHERE (kind='global' AND budget_key='global') OR (kind='workspace' AND budget_key=p_wid::text)
 OR (kind='project' AND budget_key=p_wid::text||'/'||p_pid::text)
 OR (kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text);
 UPDATE public.monitor_runs SET claimed_at=gate_now,lease_until=gate_now+interval '45 seconds' WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 UPDATE public.monitor_schedules SET last_fencing_token=new_fence,last_attempt_run_id=p_run,next_due_at=NULL,blocked_reason=NULL,updated_at=GREATEST(updated_at,gate_now)
 WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=gate_now WHERE kind='worker' AND login_name=session_user AND incarnation=p_incarnation;
 RETURN jsonb_build_object('workspace_id',p_wid,'project_id',p_pid,'monitor_id',p_mid,'run_id',p_run,'configuration_version',p_v::text,
 'activation_generation',p_g::text,'scheduler_epoch',p_epoch::text,'fencing_token',new_fence::text,'claimed_at',gate_now,'lease_until',gate_now+interval '45 seconds');
END $$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.execution_status(p_digest bytea,p_wid uuid,p_pid uuid,p_mid uuid)
RETURNS TABLE(decision text,data jsonb) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
WITH gate_time AS MATERIALIZED(SELECT clock_timestamp() AS now), identity AS MATERIALIZED (
 SELECT u.id FROM public.users u JOIN public.sessions s ON s.user_id=u.id CROSS JOIN gate_time t
 WHERE s.token_hash=p_digest AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>t.now
), scoped AS MATERIALIZED (
 SELECT m.workspace_id,m.project_id,m.id monitor_id,m.version,a.activation_generation,a.desired_state,
 s.authorized_configuration_version,s.authorized_activation_generation,s.scheduler_epoch,s.expires_at,s.next_due_at,
 c.gate,c.emergency_stop,c.expires_at deployment_expires,c.disk_used_bytes,c.disk_budget_bytes,t.now,
 EXISTS(SELECT 1 FROM public.workspace_members o JOIN public.users u ON u.id=o.user_id WHERE o.workspace_id=m.workspace_id AND o.role='owner' AND u.status='active') has_owner,
 la.attempt,lc.observation,co.observation current_observation,
 la.v attempt_v,la.g attempt_g,la.e attempt_e,la.state attempt_state,
 co.v completed_v,co.g completed_g,co.e completed_e,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.started_at IS NOT NULL AND r.physical_release_confirmed_at IS NULL) in_flight,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state IN ('unknown','completed') AND r.physical_release_confirmed_at IS NULL) quarantined
 FROM identity i JOIN public.workspace_members b ON b.user_id=i.id AND b.workspace_id=p_wid AND b.role IN ('owner','member')
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
  ORDER BY r.claimed_at DESC,r.fencing_token DESC LIMIT 1
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
 -- Current observation follows the independently gated Result pointer. A later
 -- historical audit commit cannot select itself merely by finishing last.
 LEFT JOIN LATERAL (
  SELECT jsonb_build_object('workspace_id',r.workspace_id,'project_id',r.project_id,'monitor_id',r.monitor_id,'run_id',r.run_id,
   'configuration_version',r.configuration_version::text,'activation_generation',r.activation_generation::text,'scheduler_epoch',r.scheduler_epoch::text,'fencing_token',r.fencing_token::text,
   'attempt_state',r.attempt_state,'scheduled_at',r.scheduled_at,'claimed_at',r.claimed_at,'started_at',r.started_at,'finished_at',r.finished_at,
   'duration_ms',r.duration_ms,'http_status',r.http_status,'success',r.success,'failure_code',r.failure_code,'stop_reason',r.stop_reason) observation,
   r.configuration_version v,r.activation_generation g,r.scheduler_epoch e
  FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state='completed' AND r.run_id=s.latest_result_run_id
   AND r.configuration_version=s.authorized_configuration_version AND r.activation_generation=s.authorized_activation_generation
  ORDER BY r.finished_at DESC,r.run_id DESC LIMIT 1
 ) co ON true
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
 WHEN quarantined THEN 'quarantined'
 WHEN EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE b.kind='global' AND b.budget_key='global'
 AND (b.terminal_count>=100000 OR b.retention_pending_count>=32 OR b.key_count+
  CASE WHEN EXISTS(SELECT 1 FROM public.scheduler_budget_state w WHERE w.kind='workspace' AND w.budget_key=workspace_id::text) THEN 0 ELSE 1 END+
  CASE WHEN EXISTS(SELECT 1 FROM public.scheduler_budget_state p WHERE p.kind='project' AND p.budget_key=workspace_id::text||'/'||project_id::text) THEN 0 ELSE 1 END+
  CASE WHEN EXISTS(SELECT 1 FROM public.scheduler_budget_state m WHERE m.kind='monitor' AND m.budget_key=workspace_id::text||'/'||project_id::text||'/'||monitor_id::text) THEN 0 ELSE 1 END>10000))
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE (b.kind='workspace' AND b.budget_key=workspace_id::text AND b.terminal_count>=10000)
 OR (b.kind='monitor' AND b.budget_key=workspace_id::text||'/'||project_id::text||'/'||monitor_id::text AND b.terminal_count>=100))
 OR EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=scoped.workspace_id AND r.project_id=scoped.project_id AND r.monitor_id=scoped.monitor_id
 AND r.finished_at<=now-interval '7 days') THEN 'retention_capacity'
 ELSE NULL END reason
 FROM scoped
)
SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM identity) THEN 'unauthenticated' WHEN NOT EXISTS(SELECT 1 FROM scoped) THEN 'not_found' ELSE 'ok' END,
 (SELECT jsonb_build_object('workspace_id',workspace_id,'project_id',project_id,'monitor_id',monitor_id,
 'configuration_version',version::text,'activation_generation',activation_generation::text,'scheduler_epoch',scheduler_epoch::text,
 'desired_state',desired_state,'authorization_state',authorization_state,'scheduling_state',CASE WHEN desired_state='disabled' THEN 'disabled' WHEN reason IS NOT NULL THEN 'blocked' WHEN next_due_at<=now THEN 'due' ELSE 'authorized_waiting' END,
 'effective_authorization',desired_state='enabled' AND authorization_state='valid' AND reason IS NULL,
 'expires_at',expires_at,'next_due',next_due_at,'in_flight',in_flight,'pause_semantics','start_commit','blocked_reason',reason,
 'latest_attempt',attempt,'latest_completed_observation',observation,
 'current_observation',CASE WHEN attempt_state IS DISTINCT FROM 'unknown' AND completed_v=version AND completed_g=activation_generation AND completed_e=scheduler_epoch THEN current_observation END) FROM states);
$$;
-- +goose StatementEnd

-- +goose Down
LOCK TABLE public.workspaces,public.workspace_members,public.projects,public.http_monitors,
 public.monitor_activation_states,public.monitor_schedules,public.monitor_runs,
 public.scheduler_budget_state,public.scheduler_reservations,public.scheduler_control IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.monitor_runs) OR EXISTS(SELECT 1 FROM public.scheduler_control WHERE gate<>'closed') THEN
  RAISE EXCEPTION 'capacity down requires closed gate and no retained runs' USING ERRCODE='55000';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER scheduler_run_capacity_insert ON public.monitor_runs;
DROP TRIGGER scheduler_run_capacity_update ON public.monitor_runs;
DROP TRIGGER scheduler_run_capacity_delete ON public.monitor_runs;
DROP TRIGGER scheduler_run_capacity_truncate ON public.monitor_runs;
DROP FUNCTION scheduler_private.run_capacity_accounting();
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.claim(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_uid uuid,p_membership uuid,p_v bigint,p_g bigint,p_epoch bigint,p_run uuid)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE s public.monitor_schedules%ROWTYPE; provisional timestamptz; gate_now timestamptz; observed_now timestamptz; previous_gate timestamptz; new_fence bigint;
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
 SELECT clock_timestamp(),c.last_gate_at INTO observed_now,previous_gate FROM public.scheduler_control c
 WHERE c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation;
 gate_now:=GREATEST(observed_now,s.updated_at,previous_gate);
 -- A backwards step exceeding the existing2s transaction budget cannot be
 -- absorbed into this operation; refuse rather than moving durable time back.
 IF gate_now>observed_now+interval '2 seconds' THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 PERFORM 1 FROM public.users u JOIN public.workspace_members b ON b.user_id=u.id AND b.workspace_id=p_wid
 JOIN public.projects p ON p.workspace_id=b.workspace_id AND p.id=p_pid
 JOIN public.http_monitors m ON m.workspace_id=p.workspace_id AND m.project_id=p.id AND m.id=p_mid
 JOIN public.monitor_activation_states a ON a.workspace_id=m.workspace_id AND a.project_id=m.project_id AND a.monitor_id=m.id
 JOIN public.monitor_schedules x ON x.workspace_id=m.workspace_id AND x.project_id=m.project_id AND x.monitor_id=m.id
 CROSS JOIN public.scheduler_control c
 WHERE u.id=p_uid AND u.status='active' AND b.role='owner' AND b.membership_id=p_membership
 AND m.version=p_v AND a.desired_state='enabled' AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND x.authorized_configuration_version=p_v AND x.authorized_activation_generation=p_g AND x.scheduler_epoch=p_epoch AND x.consent_protocol=1
 AND (x.last_started_at IS NULL OR x.last_started_at+m.interval_seconds*interval '1 second'<=gate_now)
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
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.execution_status(p_digest bytea,p_wid uuid,p_pid uuid,p_mid uuid)
RETURNS TABLE(decision text,data jsonb) LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
WITH gate_time AS MATERIALIZED(SELECT clock_timestamp() AS now), identity AS MATERIALIZED (
 SELECT u.id FROM public.users u JOIN public.sessions s ON s.user_id=u.id CROSS JOIN gate_time t
 WHERE s.token_hash=p_digest AND u.status='active' AND s.revoked_at IS NULL AND s.expires_at>t.now
), scoped AS MATERIALIZED (
 SELECT m.workspace_id,m.project_id,m.id monitor_id,m.version,a.activation_generation,a.desired_state,
 s.authorized_configuration_version,s.authorized_activation_generation,s.scheduler_epoch,s.expires_at,s.next_due_at,
 c.gate,c.emergency_stop,c.expires_at deployment_expires,c.disk_used_bytes,c.disk_budget_bytes,t.now,
 EXISTS(SELECT 1 FROM public.workspace_members o JOIN public.users u ON u.id=o.user_id WHERE o.workspace_id=m.workspace_id AND o.role='owner' AND u.status='active') has_owner,
 la.attempt,lc.observation,co.observation current_observation,
 la.v attempt_v,la.g attempt_g,la.e attempt_e,la.state attempt_state,
 co.v completed_v,co.g completed_g,co.e completed_e,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.started_at IS NOT NULL AND r.physical_release_confirmed_at IS NULL) in_flight,
 EXISTS(SELECT 1 FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state IN ('unknown','completed') AND r.physical_release_confirmed_at IS NULL) quarantined
 FROM identity i JOIN public.workspace_members b ON b.user_id=i.id AND b.workspace_id=p_wid AND b.role IN ('owner','member')
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
  ORDER BY r.claimed_at DESC,r.fencing_token DESC LIMIT 1
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
 -- Current observation follows the independently gated Result pointer. A later
 -- historical audit commit cannot select itself merely by finishing last.
 LEFT JOIN LATERAL (
  SELECT jsonb_build_object('workspace_id',r.workspace_id,'project_id',r.project_id,'monitor_id',r.monitor_id,'run_id',r.run_id,
   'configuration_version',r.configuration_version::text,'activation_generation',r.activation_generation::text,'scheduler_epoch',r.scheduler_epoch::text,'fencing_token',r.fencing_token::text,
   'attempt_state',r.attempt_state,'scheduled_at',r.scheduled_at,'claimed_at',r.claimed_at,'started_at',r.started_at,'finished_at',r.finished_at,
   'duration_ms',r.duration_ms,'http_status',r.http_status,'success',r.success,'failure_code',r.failure_code,'stop_reason',r.stop_reason) observation,
   r.configuration_version v,r.activation_generation g,r.scheduler_epoch e
  FROM public.monitor_runs r WHERE r.workspace_id=m.workspace_id AND r.project_id=m.project_id AND r.monitor_id=m.id AND r.attempt_state='completed' AND r.run_id=s.latest_result_run_id
   AND r.configuration_version=s.authorized_configuration_version AND r.activation_generation=s.authorized_activation_generation
  ORDER BY r.finished_at DESC,r.run_id DESC LIMIT 1
 ) co ON true
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
 'current_observation',CASE WHEN attempt_state IS DISTINCT FROM 'unknown' AND completed_v=version AND completed_g=activation_generation AND completed_e=scheduler_epoch THEN current_observation END) FROM states);
$$;
-- +goose StatementEnd
ALTER TABLE public.scheduler_budget_state DROP COLUMN retention_pending_count;
GRANT UPDATE ON public.scheduler_budget_state TO devpilot_scheduler_api_owner;
