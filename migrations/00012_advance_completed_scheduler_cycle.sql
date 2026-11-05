-- +goose Up
-- Task9.1 product-approved forward fix. Migration11 storage work remains deferred.
-- M1--10 are immutable. CREATE OR REPLACE preserves the existing owner and ACL;
-- no new role, table, column, EXECUTE or physical-release authority is granted.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.commit_result(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_run uuid,p_v bigint,p_g bigint,p_epoch bigint,p_fence bigint,p_namespace uuid,p_supervisor uuid,p_commit_id uuid,p_duration_ms integer,p_http_status integer,p_success boolean,p_failure_code text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE; gate_now timestamptz; observed_now timestamptz; keys jsonb; k record;
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 IF p_commit_id IS NULL OR p_v IS NULL OR p_g IS NULL OR p_epoch IS NULL OR p_fence IS NULL OR LEAST(p_v,p_g,p_epoch,p_fence)<1
 OR p_duration_ms IS NULL OR p_duration_ms NOT BETWEEN 0 AND 30000 OR p_success IS NULL
 OR (p_http_status IS NOT NULL AND p_http_status NOT BETWEEN 200 AND 599)
 OR (p_success AND (p_http_status IS NULL OR p_failure_code IS NOT NULL))
 OR (NOT p_success AND (p_failure_code IS NULL OR p_failure_code NOT IN ('dns_failed','blocked_target','connect_failed','tls_failed','timeout','canceled','response_limit','protocol_error','unexpected_status')))
 OR (NOT p_success AND ((p_failure_code='unexpected_status') IS DISTINCT FROM (p_http_status IS NOT NULL))) THEN
  RAISE EXCEPTION 'invalid minimal result' USING ERRCODE='22023'; END IF;
 -- Recovery takes no User/Membership/Session lock and never reauthorizes a GET.
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,NULL,NULL,false);
 SELECT * INTO r FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 -- Existing rows only: persistence may neither create nor refund a budget key.
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global' FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='workspace' AND budget_key=p_wid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='project' AND budget_key=p_wid::text||'/'||p_pid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text FOR UPDATE;
 SELECT target_keys INTO keys FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 FOR k IN SELECT kind,budget_key FROM (
  SELECT 'origin'::text kind,x->>'origin_key' budget_key FROM jsonb_array_elements(keys) x
  UNION SELECT 'ip',x->>'ip_key' FROM jsonb_array_elements(keys) x
 ) z ORDER BY CASE kind WHEN 'origin' THEN 0 ELSE 1 END,budget_key LOOP
  PERFORM 1 FROM public.scheduler_budget_state WHERE kind=k.kind AND budget_key=k.budget_key FOR UPDATE;
 END LOOP;
 SELECT * INTO q FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_control(p_incarnation);
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 -- Fresh SPI after all waits. One observed DB clock, bounded by durable
 -- timestamps as D5 requires; a backward jump cannot renew an old Lease.
 SELECT clock_timestamp() INTO observed_now;
 SELECT GREATEST(observed_now,r.claimed_at,r.started_at,q.released_at,c.last_gate_at) INTO gate_now
 FROM public.scheduler_control c WHERE c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation;
 IF gate_now>observed_now+interval '2 seconds' THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 -- A committed replay acknowledges only an immutable original fact. It does
 -- not regain Lease authority, refresh timestamps or rewrite a projection.
 IF ROW(r.configuration_version,r.activation_generation,r.scheduler_epoch,r.fencing_token) IS DISTINCT FROM ROW(p_v,p_g,p_epoch,p_fence)
 OR r.lease_owner<>session_user OR r.worker_incarnation<>p_incarnation
 OR q.namespace_incarnation IS DISTINCT FROM p_namespace OR q.supervisor_incarnation IS DISTINCT FROM p_supervisor
 OR p_namespace IS NULL OR p_supervisor IS NULL THEN
  RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 IF r.result_commit_id IS NOT NULL THEN
  IF r.attempt_state<>'completed' OR r.result_commit_id<>p_commit_id
  OR ROW(r.duration_ms,r.http_status,r.success,r.failure_code) IS DISTINCT FROM ROW(p_duration_ms,p_http_status,p_success,p_failure_code) THEN
   RAISE EXCEPTION 'result_conflict' USING ERRCODE='P0001'; END IF;
  RETURN jsonb_build_object('run_id',p_run,'commit_id',p_commit_id,'state','committed');
 END IF;

 PERFORM 1 FROM public.monitor_runs x JOIN public.monitor_schedules s USING(workspace_id,project_id,monitor_id)
 JOIN public.scheduler_control c ON c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation
 WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run
 AND ROW(x.configuration_version,x.activation_generation,x.scheduler_epoch,x.fencing_token)=ROW(p_v,p_g,p_epoch,p_fence)
 AND x.lease_owner=session_user AND x.worker_incarnation=p_incarnation AND x.lease_until>gate_now
 AND x.attempt_state IN ('leased','started') AND x.stop_reason IS NULL AND x.result_commit_id IS NULL
 AND c.expires_at>gate_now AND NOT c.emergency_stop AND gate_now>=x.claimed_at
 AND gate_now<=transaction_timestamp()+interval '2 seconds';
 IF NOT FOUND OR q.namespace_incarnation IS DISTINCT FROM p_namespace OR q.supervisor_incarnation IS DISTINCT FROM p_supervisor THEN
  RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
  IF r.attempt_state<>'started' OR q.input_consumed_at IS NULL OR q.input_consumed_at<r.started_at
  OR q.reservation_state<>'released' OR q.release_basis<>'supervisor' OR q.termination_receipt IS NULL
  OR q.termination_receipt_id IS NULL OR q.stop_key_id IS NULL OR q.termination_network_digest IS NULL
  OR q.termination_process_digest IS NULL OR q.termination_retire_digest IS NULL OR p_namespace IS NULL OR p_supervisor IS NULL
  OR r.physical_release_confirmed_at IS NULL OR r.physical_release_confirmed_at IS DISTINCT FROM q.released_at
  OR q.released_at>gate_now THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
  UPDATE public.monitor_runs SET attempt_state='completed',finished_at=gate_now,result_commit_id=p_commit_id,duration_ms=p_duration_ms,
   http_status=p_http_status,success=p_success,failure_code=p_failure_code
  WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 -- Original Run fence and current projection are separate gates. A valid old
 -- completion remains audit only after Pause, PATCH or same-version renewal.
 UPDATE public.monitor_schedules s SET latest_result_run_id=p_run
 FROM public.http_monitors m, public.monitor_activation_states a
 WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid
 AND m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid AND m.version=p_v
 AND a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid
 AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND s.scheduler_epoch=p_epoch AND s.authorized_configuration_version=p_v AND s.authorized_activation_generation=p_g
 AND s.last_attempt_run_id=p_run AND s.last_fencing_token=p_fence;

 -- Advance only this confirmed, physically released completion, with the same
 -- current authorization. No replay, renewal, backfill or resource release.
 UPDATE public.monitor_schedules s
 SET next_run_id=uuidv7(),
     next_due_at=GREATEST(gate_now + m.interval_seconds*interval '1 second',
                          s.last_started_at + m.interval_seconds*interval '1 second',
                          s.next_start_not_before),
     blocked_reason=NULL,updated_at=GREATEST(s.updated_at,gate_now)
 FROM public.http_monitors m,public.monitor_activation_states a,public.scheduler_control d
 WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid
 AND s.next_run_id=p_run AND s.last_attempt_run_id=p_run AND s.last_fencing_token=p_fence
 AND s.scheduler_epoch=p_epoch AND s.authorized_configuration_version=p_v
 AND s.authorized_activation_generation=p_g AND s.consent_protocol=1 AND s.expires_at>gate_now
 AND m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid AND m.version=p_v
 AND a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid
 AND a.desired_state='enabled' AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND EXISTS(SELECT 1 FROM public.workspace_members b JOIN public.users u ON u.id=b.user_id
            WHERE b.workspace_id=p_wid AND b.role='owner' AND u.status='active')
 AND d.control_id='deployment' AND d.gate='fixture' AND NOT d.emergency_stop
 AND d.expires_at>gate_now AND d.disk_used_bytes<d.disk_budget_bytes*0.8
 AND NOT EXISTS(SELECT 1 FROM public.scheduler_control h WHERE h.kind='http' AND h.routable
                AND (h.projection_protocol IS DISTINCT FROM 'intent-v2' OR NOT h.status_ready
                     OR h.expires_at IS NULL OR h.expires_at<=gate_now));

 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=GREATEST(last_gate_at,gate_now)
 WHERE kind='worker' AND login_name=session_user AND incarnation=p_incarnation;
 RETURN jsonb_build_object('run_id',p_run,'commit_id',p_commit_id,'state','committed');
END $$;
-- +goose StatementEnd

-- +goose Down
-- Restore the exact accepted definition; never use Down on a running deployment.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND (gate<>'closed' OR NOT emergency_stop))
 OR EXISTS(SELECT 1 FROM public.monitor_runs WHERE physical_release_confirmed_at IS NULL) THEN
  RAISE EXCEPTION 'scheduler downgrade requires closed gate and physical stop' USING ERRCODE='55000';
 END IF;
END $$;
-- +goose StatementEnd
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION scheduler_private.commit_result(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,p_run uuid,p_v bigint,p_g bigint,p_epoch bigint,p_fence bigint,p_namespace uuid,p_supervisor uuid,p_commit_id uuid,p_duration_ms integer,p_http_status integer,p_success boolean,p_failure_code text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE; gate_now timestamptz; observed_now timestamptz; keys jsonb; k record;
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 IF p_commit_id IS NULL OR p_v IS NULL OR p_g IS NULL OR p_epoch IS NULL OR p_fence IS NULL OR LEAST(p_v,p_g,p_epoch,p_fence)<1
 OR p_duration_ms IS NULL OR p_duration_ms NOT BETWEEN 0 AND 30000 OR p_success IS NULL
 OR (p_http_status IS NOT NULL AND p_http_status NOT BETWEEN 200 AND 599)
 OR (p_success AND (p_http_status IS NULL OR p_failure_code IS NOT NULL))
 OR (NOT p_success AND (p_failure_code IS NULL OR p_failure_code NOT IN ('dns_failed','blocked_target','connect_failed','tls_failed','timeout','canceled','response_limit','protocol_error','unexpected_status')))
 OR (NOT p_success AND ((p_failure_code='unexpected_status') IS DISTINCT FROM (p_http_status IS NOT NULL))) THEN
  RAISE EXCEPTION 'invalid minimal result' USING ERRCODE='22023'; END IF;
 -- Recovery takes no User/Membership/Session lock and never reauthorizes a GET.
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,NULL,NULL,false);
 SELECT * INTO r FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 -- Existing rows only: persistence may neither create nor refund a budget key.
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global' FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='workspace' AND budget_key=p_wid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='project' AND budget_key=p_wid::text||'/'||p_pid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text FOR UPDATE;
 SELECT target_keys INTO keys FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 FOR k IN SELECT kind,budget_key FROM (
  SELECT 'origin'::text kind,x->>'origin_key' budget_key FROM jsonb_array_elements(keys) x
  UNION SELECT 'ip',x->>'ip_key' FROM jsonb_array_elements(keys) x
 ) z ORDER BY CASE kind WHEN 'origin' THEN 0 ELSE 1 END,budget_key LOOP
  PERFORM 1 FROM public.scheduler_budget_state WHERE kind=k.kind AND budget_key=k.budget_key FOR UPDATE;
 END LOOP;
 SELECT * INTO q FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 PERFORM scheduler_private.lock_control(p_incarnation);
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 -- Fresh SPI after all waits. One observed DB clock, bounded by durable
 -- timestamps as D5 requires; a backward jump cannot renew an old Lease.
 SELECT clock_timestamp() INTO observed_now;
 SELECT GREATEST(observed_now,r.claimed_at,r.started_at,q.released_at,c.last_gate_at) INTO gate_now
 FROM public.scheduler_control c WHERE c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation;
 IF gate_now>observed_now+interval '2 seconds' THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 -- A committed replay acknowledges only an immutable original fact. It does
 -- not regain Lease authority, refresh timestamps or rewrite a projection.
 IF ROW(r.configuration_version,r.activation_generation,r.scheduler_epoch,r.fencing_token) IS DISTINCT FROM ROW(p_v,p_g,p_epoch,p_fence)
 OR r.lease_owner<>session_user OR r.worker_incarnation<>p_incarnation
 OR q.namespace_incarnation IS DISTINCT FROM p_namespace OR q.supervisor_incarnation IS DISTINCT FROM p_supervisor
 OR p_namespace IS NULL OR p_supervisor IS NULL THEN
  RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
 IF r.result_commit_id IS NOT NULL THEN
  IF r.attempt_state<>'completed' OR r.result_commit_id<>p_commit_id
  OR ROW(r.duration_ms,r.http_status,r.success,r.failure_code) IS DISTINCT FROM ROW(p_duration_ms,p_http_status,p_success,p_failure_code) THEN
   RAISE EXCEPTION 'result_conflict' USING ERRCODE='P0001'; END IF;
  RETURN jsonb_build_object('run_id',p_run,'commit_id',p_commit_id,'state','committed');
 END IF;

 PERFORM 1 FROM public.monitor_runs x JOIN public.monitor_schedules s USING(workspace_id,project_id,monitor_id)
 JOIN public.scheduler_control c ON c.kind='worker' AND c.login_name=session_user AND c.incarnation=p_incarnation
 WHERE x.workspace_id=p_wid AND x.project_id=p_pid AND x.monitor_id=p_mid AND x.run_id=p_run
 AND ROW(x.configuration_version,x.activation_generation,x.scheduler_epoch,x.fencing_token)=ROW(p_v,p_g,p_epoch,p_fence)
 AND x.lease_owner=session_user AND x.worker_incarnation=p_incarnation AND x.lease_until>gate_now
 AND x.attempt_state IN ('leased','started') AND x.stop_reason IS NULL AND x.result_commit_id IS NULL
 AND c.expires_at>gate_now AND NOT c.emergency_stop AND gate_now>=x.claimed_at
 AND gate_now<=transaction_timestamp()+interval '2 seconds';
 IF NOT FOUND OR q.namespace_incarnation IS DISTINCT FROM p_namespace OR q.supervisor_incarnation IS DISTINCT FROM p_supervisor THEN
  RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
  IF r.attempt_state<>'started' OR q.input_consumed_at IS NULL OR q.input_consumed_at<r.started_at
  OR q.reservation_state<>'released' OR q.release_basis<>'supervisor' OR q.termination_receipt IS NULL
  OR q.termination_receipt_id IS NULL OR q.stop_key_id IS NULL OR q.termination_network_digest IS NULL
  OR q.termination_process_digest IS NULL OR q.termination_retire_digest IS NULL OR p_namespace IS NULL OR p_supervisor IS NULL
  OR r.physical_release_confirmed_at IS NULL OR r.physical_release_confirmed_at IS DISTINCT FROM q.released_at
  OR q.released_at>gate_now THEN RAISE EXCEPTION 'candidate_invalid' USING ERRCODE='P0001'; END IF;
  UPDATE public.monitor_runs SET attempt_state='completed',finished_at=gate_now,result_commit_id=p_commit_id,duration_ms=p_duration_ms,
   http_status=p_http_status,success=p_success,failure_code=p_failure_code
  WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=p_run;
 -- Original Run fence and current projection are separate gates. A valid old
 -- completion remains audit only after Pause, PATCH or same-version renewal.
 UPDATE public.monitor_schedules s SET latest_result_run_id=p_run
 FROM public.http_monitors m, public.monitor_activation_states a
 WHERE s.workspace_id=p_wid AND s.project_id=p_pid AND s.monitor_id=p_mid
 AND m.workspace_id=p_wid AND m.project_id=p_pid AND m.id=p_mid AND m.version=p_v
 AND a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid
 AND a.activation_generation=p_g AND a.activation_configuration_version=p_v
 AND s.scheduler_epoch=p_epoch AND s.authorized_configuration_version=p_v AND s.authorized_activation_generation=p_g
 AND s.last_attempt_run_id=p_run AND s.last_fencing_token=p_fence;

 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=GREATEST(last_gate_at,gate_now)
 WHERE kind='worker' AND login_name=session_user AND incarnation=p_incarnation;
 RETURN jsonb_build_object('run_id',p_run,'commit_id',p_commit_id,'state','committed');
END $$;
-- +goose StatementEnd
