-- +goose Up
-- Functional read authorization only: no new table/index/role or base-table
-- grant. Reuse the existing API owner and live execution-status identity gate.
-- +goose StatementBegin
CREATE FUNCTION scheduler_private.monitor_history(
 p_digest bytea,p_wid uuid,p_pid uuid,p_mid uuid,p_limit integer,p_state text,
 p_before_at timestamptz,p_before_id uuid,p_upper_at timestamptz,p_upper_id uuid)
RETURNS TABLE(decision text,data jsonb) LANGUAGE plpgsql STABLE SECURITY DEFINER
SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF p_limit IS NULL OR p_limit NOT BETWEEN 1 AND 50 OR p_state IS NULL
 OR p_state NOT IN ('all','success','failure','unknown','running','canceled','expired')
 OR (p_before_at IS NULL)<>(p_before_id IS NULL) OR (p_upper_at IS NULL)<>(p_upper_id IS NULL)
 OR (p_before_at IS NOT NULL AND p_upper_at IS NULL) THEN
  RAISE EXCEPTION 'invalid history query' USING ERRCODE='22023';
 END IF;
 RETURN QUERY
 WITH access AS MATERIALIZED (
  SELECT * FROM scheduler_private.execution_status(p_digest,p_wid,p_pid,p_mid)
 ), boundary AS MATERIALIZED (
  SELECT r.claimed_at,r.run_id FROM public.monitor_runs r,access a
  WHERE a.decision='ok' AND r.workspace_id=p_wid AND r.project_id=p_pid AND r.monitor_id=p_mid
   AND r.started_at IS NOT NULL
   AND (p_upper_at IS NULL OR ROW(r.claimed_at,r.run_id)<=ROW(p_upper_at,p_upper_id))
  ORDER BY r.claimed_at DESC,r.run_id DESC LIMIT 1
 ), page AS MATERIALIZED (
  SELECT r.claimed_at,r.run_id,jsonb_build_object(
    'id',r.run_id,'configuration_version',r.configuration_version::text,
    'checked_at',r.started_at,'finished_at',r.finished_at,
    'status',CASE WHEN r.attempt_state='completed' THEN CASE WHEN r.success THEN 'success' ELSE 'failure' END
                  WHEN r.attempt_state='started' THEN 'running' ELSE r.attempt_state END,
    'http_status',r.http_status,'duration_ms',r.duration_ms,'failure_code',r.failure_code) item
  FROM public.monitor_runs r,access a,boundary b
  WHERE a.decision='ok' AND r.workspace_id=p_wid AND r.project_id=p_pid AND r.monitor_id=p_mid
   AND r.started_at IS NOT NULL
   AND ROW(r.claimed_at,r.run_id)<=ROW(b.claimed_at,b.run_id)
   AND (p_before_at IS NULL OR ROW(r.claimed_at,r.run_id)<ROW(p_before_at,p_before_id))
   AND (p_state='all' OR (p_state='success' AND r.attempt_state='completed' AND r.success)
    OR (p_state='failure' AND r.attempt_state='completed' AND NOT r.success)
    OR (p_state='running' AND r.attempt_state='started') OR r.attempt_state=p_state)
  ORDER BY r.claimed_at DESC,r.run_id DESC LIMIT p_limit+1
 )
 SELECT a.decision,CASE WHEN a.decision='ok' THEN jsonb_build_object(
  'configuration_version',a.data->>'configuration_version',
  'upper_at',COALESCE(p_upper_at,(SELECT b.claimed_at FROM boundary b)),
  'upper_id',COALESCE(p_upper_id,(SELECT b.run_id FROM boundary b)),
  'rows',COALESCE((SELECT jsonb_agg(jsonb_build_object('item',p.item,'position_at',p.claimed_at,'position_id',p.run_id)
                   ORDER BY p.claimed_at DESC,p.run_id DESC) FROM page p),'[]'::jsonb)) END
 FROM access a;
END $$;
-- +goose StatementEnd
ALTER FUNCTION scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid) OWNER TO devpilot_scheduler_api_owner;
REVOKE ALL ON FUNCTION scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid) TO devpilot_scheduler_api;

-- +goose Down
DROP FUNCTION scheduler_private.monitor_history(bytea,uuid,uuid,uuid,integer,text,timestamptz,uuid,timestamptz,uuid);
