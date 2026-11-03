-- +goose Up
-- Task8.2: three approved Controller-only entries. No changes to M1--9 guards.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.retention_candidates(p_incarnation uuid,p_cursor text,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE c jsonb; phase text:='scope'; w uuid; p uuid; m uuid; k text; b text; rows jsonb; tail jsonb; next_cursor text;
BEGIN
 PERFORM scheduler_private.system_identity('controller',p_incarnation);
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 64 OR octet_length(p_cursor)>512 THEN
  RAISE EXCEPTION 'invalid cleanup cursor' USING ERRCODE='22023'; END IF;
 IF p_cursor IS NOT NULL THEN
  c:=p_cursor::jsonb;
  IF jsonb_typeof(c)<>'object' OR NOT(c ? 'phase') THEN RAISE EXCEPTION 'invalid cleanup cursor' USING ERRCODE='22023'; END IF;
  phase:=c->>'phase';
  IF phase='scope' AND (SELECT count(*) FROM jsonb_object_keys(c))=4 THEN
   w:=(c->>'workspace_id')::uuid;p:=(c->>'project_id')::uuid;m:=(c->>'monitor_id')::uuid;
   IF w IS NULL OR p IS NULL OR m IS NULL THEN RAISE EXCEPTION 'invalid cleanup cursor' USING ERRCODE='22023'; END IF;
  ELSIF phase='target' AND (SELECT count(*) FROM jsonb_object_keys(c))=3 THEN
   k:=c->>'kind';b:=c->>'budget_key';
   IF k NOT IN ('origin','ip') OR b !~ '^[0-9a-f]{64}$' OR k IS NULL OR b IS NULL THEN
    RAISE EXCEPTION 'invalid cleanup cursor' USING ERRCODE='22023'; END IF;
  ELSE RAISE EXCEPTION 'invalid cleanup cursor' USING ERRCODE='22023'; END IF;
 END IF;
 IF phase='scope' THEN
  SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY workspace_id,project_id,monitor_id),'[]') INTO rows FROM (
   SELECT s.workspace_id,s.project_id,s.monitor_id FROM public.monitor_schedules s
   WHERE w IS NULL OR ROW(s.workspace_id,s.project_id,s.monitor_id)>ROW(w,p,m)
   ORDER BY s.workspace_id,s.project_id,s.monitor_id LIMIT p_limit
  ) x;
  IF jsonb_array_length(rows)>0 THEN
   tail:=rows->(jsonb_array_length(rows)-1);next_cursor:=(tail||jsonb_build_object('phase','scope'))::text;
   RETURN jsonb_build_object('items',rows,'next_cursor',next_cursor);
  END IF;
  phase:='target';
 END IF;
 SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY kind,budget_key),'[]') INTO rows FROM (
  SELECT t.kind,t.budget_key FROM public.scheduler_budget_state t WHERE t.kind IN ('origin','ip')
   AND (k IS NULL OR ROW(t.kind,t.budget_key)>ROW(k,b)) ORDER BY t.kind,t.budget_key LIMIT p_limit
 ) x;
 IF jsonb_array_length(rows)>0 THEN
  tail:=rows->(jsonb_array_length(rows)-1);next_cursor:=(tail||jsonb_build_object('phase','target'))::text;
 END IF;
 RETURN jsonb_build_object('items',rows,'next_cursor',next_cursor);
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.retention_candidates(uuid,text,integer) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.retention_candidates(uuid,text,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.retention_candidates(uuid,text,integer) TO devpilot_scheduler_controller;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.cleanup_monitor(p_incarnation uuid,p_wid uuid,p_pid uuid,p_mid uuid,
 p_after_finished timestamptz,p_after_run uuid,p_limit integer)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE s public.monitor_schedules%ROWTYPE; r public.monitor_runs%ROWTYPE; q public.scheduler_reservations%ROWTYPE;
 ids uuid[]; eligible uuid[]:='{}'; keys record; n timestamptz; observed timestamptz;
 deleted integer:=0; written bigint:=0; affected integer; pressure boolean; last_finished timestamptz; last_run uuid;
BEGIN
 PERFORM scheduler_private.system_identity('controller',p_incarnation);
 IF p_wid IS NULL OR p_pid IS NULL OR p_mid IS NULL OR p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 8
 OR (p_after_finished IS NULL)<>(p_after_run IS NULL) OR NOT isfinite(COALESCE(p_after_finished,clock_timestamp())) THEN
  RAISE EXCEPTION 'invalid cleanup scope' USING ERRCODE='22023'; END IF;
 -- Reject a second state operation before it can take any new parent/Run lock
 -- while this transaction already owns a budget. The final locked check stays.
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND last_transaction=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'one cleanup per transaction' USING ERRCODE='25000'; END IF;
 PERFORM scheduler_private.lock_parents(p_wid,p_pid,p_mid,NULL,NULL,false);
 SELECT * INTO s FROM public.monitor_schedules WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid;
 -- Candidate selection is only a hint. Every selected existing Run is locked
 -- before G; no additional Run is acquired while holding budgets/reservations.
 SELECT array_agg(x.run_id) INTO ids FROM (
  SELECT run_id FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid
   AND finished_at IS NOT NULL AND (p_after_finished IS NULL OR ROW(finished_at,run_id)>ROW(p_after_finished,p_after_run))
  ORDER BY finished_at,run_id LIMIT p_limit
 ) x;
 PERFORM 1 FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid
  AND run_id=ANY(ids) ORDER BY run_id FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global' FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='workspace' AND budget_key=p_wid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='project' AND budget_key=p_wid::text||'/'||p_pid::text FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text FOR UPDATE;
 FOR keys IN SELECT z.kind,z.budget_key FROM public.scheduler_reservations a
  CROSS JOIN LATERAL jsonb_array_elements(a.target_keys) j
  CROSS JOIN LATERAL (VALUES ('origin'::text,j->>'origin_key'),('ip'::text,j->>'ip_key')) z(kind,budget_key)
  WHERE a.workspace_id=p_wid AND a.project_id=p_pid AND a.monitor_id=p_mid AND a.run_id=ANY(ids)
  GROUP BY z.kind,z.budget_key ORDER BY CASE z.kind WHEN 'origin' THEN 0 ELSE 1 END,z.budget_key LOOP
  PERFORM 1 FROM public.scheduler_budget_state WHERE kind=keys.kind AND budget_key=keys.budget_key FOR UPDATE;
 END LOOP;
 PERFORM 1 FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid
  AND run_id=ANY(ids) ORDER BY run_id FOR UPDATE;
 PERFORM 1 FROM public.scheduler_control WHERE control_id='deployment' FOR SHARE;
 PERFORM 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user ORDER BY control_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND last_transaction=pg_current_xact_id()) THEN RAISE EXCEPTION 'one cleanup per transaction' USING ERRCODE='25000'; END IF;
 -- Fresh SPI commands after ALL waits. Never reuse an outer statement snapshot.
 PERFORM scheduler_private.system_identity('controller',p_incarnation);
 observed:=clock_timestamp();n:=observed;
 SELECT GREATEST(n,max(last_refill_at)) INTO n FROM public.scheduler_budget_state WHERE
  kind='global' OR (kind='workspace' AND budget_key=p_wid::text) OR (kind='project' AND budget_key=p_wid::text||'/'||p_pid::text)
  OR (kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text);
 SELECT GREATEST(n,last_gate_at) INTO n FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND incarnation=p_incarnation;
 IF n>observed+interval '2 seconds' THEN RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 IF NOT EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global') THEN
  RAISE EXCEPTION 'capacity ledger inconsistent' USING ERRCODE='23514'; END IF;
 IF (SELECT count(*) FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user)>50 THEN
  RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=GREATEST(last_gate_at,n)
  WHERE kind='controller' AND login_name=session_user;
 GET DIAGNOSTICS written=ROW_COUNT;
 IF written>50 THEN RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
 IF s.expires_at>n OR s.next_start_not_before>n OR s.last_started_at>n-interval '1 hour'
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND (keep_until>n OR not_before>n)) THEN
  IF written>100 OR clock_timestamp()>transaction_timestamp()+interval '2 seconds' THEN
   RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
  RETURN jsonb_build_object('deleted_runs',0,'reason','protected','rows_written',written,'next_finished',NULL,'next_run',NULL);
 END IF;
 SELECT EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE (kind='global' AND terminal_count>=100000)
  OR (kind='workspace' AND budget_key=p_wid::text AND terminal_count>=10000)
  OR (kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND terminal_count>=100)) INTO pressure;
 FOR r IN SELECT * FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid
  AND run_id=ANY(ids) ORDER BY finished_at,run_id LOOP
  last_finished:=r.finished_at;last_run:=r.run_id;
  IF r.attempt_state IN ('leased','started') OR r.physical_release_confirmed_at IS NULL
   OR (NOT pressure AND r.finished_at>n-interval '7 days') THEN CONTINUE; END IF;
  SELECT * INTO q FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=r.run_id;
  -- Missing prepared evidence is never guessed from an exited process or lease.
  IF NOT FOUND OR q.reservation_state<>'released' OR q.released_at IS DISTINCT FROM r.physical_release_confirmed_at
   OR (q.release_basis='supervisor' AND (q.termination_receipt_id IS NULL OR q.termination_receipt IS NULL
    OR q.namespace_incarnation IS NULL OR q.supervisor_incarnation IS NULL OR q.stop_key_id IS NULL
    OR q.termination_network_digest IS NULL OR q.termination_process_digest IS NULL OR q.termination_retire_digest IS NULL))
   OR (q.release_basis='never_prepared' AND (q.namespace_incarnation IS NOT NULL OR q.supervisor_incarnation IS NOT NULL
    OR q.prepared_at IS NOT NULL OR q.preparation_digest IS NOT NULL OR q.permit_deadline IS NOT NULL
    OR q.input_consumed_at IS NOT NULL OR q.target_permit_id IS NOT NULL OR q.target_permit_committed_at IS NOT NULL
    OR q.permit_installed_at IS NOT NULL OR q.origin_key IS NOT NULL OR q.ip_key IS NOT NULL
    OR q.target_keys<>'[]' OR q.stop_key_id IS NOT NULL OR r.started_at IS NOT NULL)) THEN CONTINUE; END IF;
  IF EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE (b.kind='origin' AND (b.budget_key=q.origin_key OR q.target_keys @> jsonb_build_array(jsonb_build_object('origin_key',b.budget_key))))
    OR (b.kind='ip' AND (b.budget_key=q.ip_key OR q.target_keys @> jsonb_build_array(jsonb_build_object('ip_key',b.budget_key))))
   GROUP BY b.kind,b.budget_key HAVING bool_or(b.in_use<>0 OR b.keep_until>n OR b.not_before>n)) THEN CONTINUE; END IF;
  eligible:=array_append(eligible,r.run_id);
 END LOOP;
 -- Clear both pointers if the latest attempt retires: no old success fallback.
 UPDATE public.monitor_schedules SET last_attempt_run_id=CASE WHEN last_attempt_run_id=ANY(eligible) THEN NULL ELSE last_attempt_run_id END,
  latest_result_run_id=CASE WHEN last_attempt_run_id=ANY(eligible) OR latest_result_run_id=ANY(eligible) THEN NULL ELSE latest_result_run_id END,
  updated_at=GREATEST(updated_at,n) WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid
  AND (last_attempt_run_id=ANY(eligible) OR latest_result_run_id=ANY(eligible));
 GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected;
 DELETE FROM public.scheduler_reservations WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=ANY(eligible);
 GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected;
 DELETE FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid AND run_id=ANY(eligible);
 GET DIAGNOSTICS deleted=ROW_COUNT;
 -- M9 BEFORE DELETE writes exactly G/W/M once per Run. It is unchanged.
 written:=written+deleted*4;
 IF NOT EXISTS(SELECT 1 FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid) THEN
  DELETE FROM public.monitor_schedules WHERE workspace_id=p_wid AND project_id=p_pid AND monitor_id=p_mid;
  GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected*2; -- Schedule + global count Trigger
  DELETE FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text
   AND in_use=0 AND terminal_count=0 AND keep_until<=n AND (not_before IS NULL OR not_before<=n);
  GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected*2; -- key + global count Trigger
  DELETE FROM public.scheduler_budget_state b WHERE b.kind='project' AND b.budget_key=p_wid::text||'/'||p_pid::text
   AND b.in_use=0 AND b.keep_until<=n AND (b.not_before IS NULL OR b.not_before<=n)
   AND NOT EXISTS(SELECT 1 FROM public.monitor_schedules WHERE workspace_id=p_wid AND project_id=p_pid)
   AND NOT EXISTS(SELECT 1 FROM public.monitor_runs WHERE workspace_id=p_wid AND project_id=p_pid);
  GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected*2;
  DELETE FROM public.scheduler_budget_state b WHERE b.kind='workspace' AND b.budget_key=p_wid::text
   AND b.in_use=0 AND b.terminal_count=0 AND b.keep_until<=n AND (b.not_before IS NULL OR b.not_before<=n)
   AND NOT EXISTS(SELECT 1 FROM public.monitor_schedules WHERE workspace_id=p_wid)
   AND NOT EXISTS(SELECT 1 FROM public.monitor_runs WHERE workspace_id=p_wid);
  GET DIAGNOSTICS affected=ROW_COUNT;written:=written+affected*2;
 END IF;
 IF written>100 OR clock_timestamp()>transaction_timestamp()+interval '2 seconds' THEN
  RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
 RETURN jsonb_build_object('deleted_runs',deleted,'rows_written',written,'next_finished',last_finished,'next_run',last_run);
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer) TO devpilot_scheduler_controller;

-- +goose StatementBegin
CREATE FUNCTION scheduler_private.cleanup_target_key(p_incarnation uuid,p_kind text,p_key text)
RETURNS jsonb LANGUAGE plpgsql VOLATILE SECURITY DEFINER
SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
DECLARE n timestamptz; deleted integer; written bigint;
BEGIN
 PERFORM scheduler_private.system_identity('controller',p_incarnation);
 IF p_kind IS NULL OR p_kind NOT IN ('origin','ip') OR p_key IS NULL OR p_key !~ '^[0-9a-f]{64}$' THEN
  RAISE EXCEPTION 'invalid cleanup key' USING ERRCODE='22023'; END IF;
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND last_transaction=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'one cleanup per transaction' USING ERRCODE='25000'; END IF;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind='global' AND budget_key='global' FOR UPDATE;
 PERFORM 1 FROM public.scheduler_budget_state WHERE kind=p_kind AND budget_key=p_key FOR UPDATE;
 PERFORM 1 FROM public.scheduler_control WHERE control_id='deployment' FOR SHARE;
 PERFORM 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user ORDER BY control_id FOR UPDATE;
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND last_transaction=pg_current_xact_id()) THEN RAISE EXCEPTION 'one cleanup per transaction' USING ERRCODE='25000'; END IF;
 PERFORM scheduler_private.system_identity('controller',p_incarnation);
 n:=clock_timestamp();
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user AND incarnation=p_incarnation AND last_gate_at>n+interval '2 seconds')
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind=p_kind AND budget_key=p_key AND last_refill_at>n+interval '2 seconds') THEN
  RAISE EXCEPTION 'clock_untrusted' USING ERRCODE='P0001'; END IF;
 IF (SELECT count(*) FROM public.scheduler_control WHERE kind='controller' AND login_name=session_user)>50 THEN
  RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
 UPDATE public.scheduler_control SET last_transaction=pg_current_xact_id(),last_gate_at=GREATEST(last_gate_at,n)
  WHERE kind='controller' AND login_name=session_user;
 GET DIAGNOSTICS written=ROW_COUNT;
 DELETE FROM public.scheduler_budget_state b WHERE b.kind=p_kind AND b.budget_key=p_key AND b.in_use=0
  AND b.keep_until<=n AND (b.not_before IS NULL OR b.not_before<=n) AND b.last_refill_at<=n-interval '1 hour'
  AND NOT EXISTS(SELECT 1 FROM public.scheduler_control WHERE kind='deployment' AND target_keys_rotated_at>n-interval '1 hour')
  AND NOT EXISTS(SELECT 1 FROM public.scheduler_reservations q WHERE (p_kind='origin' AND (q.origin_key=p_key OR q.target_keys @> jsonb_build_array(jsonb_build_object('origin_key',p_key))))
    OR (p_kind='ip' AND (q.ip_key=p_key OR q.target_keys @> jsonb_build_array(jsonb_build_object('ip_key',p_key)))));
 GET DIAGNOSTICS deleted=ROW_COUNT;
 written:=written+deleted*2; -- key DELETE and its unchanged global-count Trigger
 IF written>100 OR clock_timestamp()>transaction_timestamp()+interval '2 seconds' THEN
  RAISE EXCEPTION 'cleanup transaction bound' USING ERRCODE='54000'; END IF;
 RETURN jsonb_build_object('deleted_keys',deleted,'rows_written',written);
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.cleanup_target_key(uuid,text,text) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.cleanup_target_key(uuid,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.cleanup_target_key(uuid,text,text) TO devpilot_scheduler_controller;

-- +goose Down
-- Disposable maintenance only. Never remove cleanup protocol with retained Runs.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE gate<>'closed' AND kind='deployment')
 OR EXISTS(SELECT 1 FROM public.monitor_runs) THEN
  RAISE EXCEPTION 'cleanup downgrade requires closed gate and no retained runs' USING ERRCODE='55000'; END IF;
END $$;
-- +goose StatementEnd
DROP FUNCTION scheduler_private.cleanup_target_key(uuid,text,text);
DROP FUNCTION scheduler_private.cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer);
DROP FUNCTION scheduler_private.retention_candidates(uuid,text,integer);
