#!/usr/bin/env python3
"""Emit an inactive forward candidate. Never connects to a database/applies SQL."""
import hashlib,json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
EXPECTED={'authorize':1,'execution_status':1,'claim':1,'start_run':1,'prepare_run':1,'consume_input':1,'reconcile_claim':1,'reserve_target':1,'commit_result':1,'recover_started':1}
latest={}
for p in sorted((ROOT/'migrations').glob('*.sql')):
 for m in re.finditer(r'CREATE(?: OR REPLACE)? FUNCTION scheduler_private\.(\w+)\(.*?\$\$;',p.read_text().split('-- +goose Down')[0],re.S):latest[m[1]]=(p,m[0])
pattern=re.compile(r"((?:\b\w+\.)?gate)\s*(=|<>)\s*'fixture'")
assert {n:len(pattern.findall(s)) for n,(_,s) in latest.items() if pattern.search(s)}==EXPECTED
header='''-- INACTIVE CANDIDATE for future Migration14. Not scanned/applied by Goose.
-- Separate approval required even for a disposable test DB/private Gate.
-- No Migration11; Migration1-13 files remain untouched.
BEGIN;
SET LOCAL statement_timeout='2s';
SET LOCAL transaction_timeout='2s';
SET LOCAL lock_timeout='250ms';
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND (gate<>'closed' OR NOT emergency_stop))
 OR EXISTS(SELECT 1 FROM public.monitor_runs WHERE physical_release_confirmed_at IS NULL)
 THEN RAISE EXCEPTION 'closed gate and independent physical stop required' USING ERRCODE='55000'; END IF;
END $$;
ALTER TABLE public.scheduler_control ADD COLUMN execution_mode text NOT NULL DEFAULT 'fixture' CHECK(execution_mode IN ('fixture','private-isolated'));
ALTER TABLE public.scheduler_control DROP CONSTRAINT scheduler_control_gate_check;
ALTER TABLE public.scheduler_control ADD CONSTRAINT scheduler_control_gate_check CHECK(gate IN ('closed','fixture','private-isolated'));

-- Internal predicate: no runtime EXECUTE. Admission matches the registered
-- incarnation mode. A private incarnation can NEVER use the fixture Gate.
CREATE FUNCTION scheduler_private.private_gate_allows(p_gate text,p_incarnation uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private AS $$
BEGIN
 IF p_gate IS NULL OR p_gate NOT IN ('fixture','private-isolated') THEN RETURN false; END IF;
 IF p_gate='private-isolated' AND NOT EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND execution_mode='private-isolated') THEN RETURN false; END IF;
 IF p_incarnation IS NULL THEN RETURN true; END IF;
 RETURN EXISTS(SELECT 1 FROM public.scheduler_control WHERE incarnation=p_incarnation AND login_name=session_user AND execution_mode=p_gate);
END $$;
ALTER FUNCTION scheduler_private.private_gate_allows(text,uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.private_gate_allows(text,uuid) FROM PUBLIC;

-- Only new runtime privilege: own-incarnation boolean readiness, not row data.
CREATE FUNCTION scheduler_private.private_worker_ready(p_incarnation uuid)
RETURNS boolean LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path=pg_catalog,scheduler_private SET lock_timeout='250ms' AS $$
BEGIN
 PERFORM scheduler_private.system_identity('worker',p_incarnation);
 RETURN EXISTS(SELECT 1 FROM public.scheduler_control d WHERE d.control_id='deployment' AND d.gate='private-isolated' AND NOT d.emergency_stop AND d.expires_at>clock_timestamp() AND scheduler_private.private_gate_allows(d.gate,p_incarnation));
END $$;
ALTER FUNCTION scheduler_private.private_worker_ready(uuid) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.private_worker_ready(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.private_worker_ready(uuid) TO devpilot_scheduler_worker;
'''
blocks=[header];report=[]
for n in EXPECTED:
 p,s=latest[n];inc='p_incarnation' if re.search(r'\bp_incarnation uuid',s.split('RETURNS')[0]) else 'NULL::uuid'
 def change(m):
  if inc=='NULL::uuid':
   # Existing API-definer owner has SELECT on scheduler_control but deliberately
   # no EXECUTE on the scheduler-owner-only helper. Equivalent inline predicate.
   expr=f"COALESCE(({m[1]}='fixture' OR ({m[1]}='private-isolated' AND EXISTS(SELECT 1 FROM public.scheduler_control private_mode WHERE private_mode.control_id='deployment' AND private_mode.execution_mode='private-isolated'))),false)"
  else:expr=f'scheduler_private.private_gate_allows({m[1]},{inc})'
  return 'NOT '+expr if m[2]=='<>' else expr
 patched=pattern.sub(change,s)
 patched=re.sub(r'^CREATE(?: OR REPLACE)? FUNCTION','CREATE OR REPLACE FUNCTION',patched)
 blocks.append(f'\n-- {n}: only the Gate predicate changes; all other statements unchanged.\n'+patched+'\n')
 report.append(dict(function=n,source=str(p.relative_to(ROOT)),source_sha256=hashlib.sha256(p.read_bytes()).hexdigest(),old_predicate=pattern.findall(s),identity_argument=inc,predicate_strategy='inline existing SELECT' if inc=='NULL::uuid' else 'owner-only helper',changes=EXPECTED[n]))
blocks.append('\n-- Deliberately does NOT register/open private mode or create LOGINs.\nCOMMIT;\n')
text=''.join(blocks);dest=ROOT/'deploy/privateloop/private-mode.sql.candidate';
if '--check' in sys.argv:assert dest.read_text()==text
else:dest.write_text(text)
report_json=json.dumps(dict(candidate_sha256=hashlib.sha256(text.encode()).hexdigest(),new_columns=['scheduler_control.execution_mode'],gate_values_added=['private-isolated'],new_functions=['private_gate_allows(text,uuid) [owner only]','private_worker_ready(uuid) [worker boolean]'],grants=['EXECUTE private_worker_ready(uuid) TO devpilot_scheduler_worker'],new_roles=[],direct_table_column_grants=[],functions=report),indent=2)+'\n'
if '--check' in sys.argv:assert (ROOT/'deploy/privateloop/private-mode-diff.json').read_text()==report_json
else:(ROOT/'deploy/privateloop/private-mode-diff.json').write_text(report_json)
print(json.dumps(dict(path=str(dest.relative_to(ROOT)),bytes=len(text),functions=len(report),sha256=hashlib.sha256(text.encode()).hexdigest())))
