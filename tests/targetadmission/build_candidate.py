#!/usr/bin/env python3
"""Generate inactive M16; no database connection or activation."""
import hashlib,json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
paths=[ROOT/'deploy/privateloop/private-mode.sql.candidate',ROOT/'deploy/capacity/continuous-capacity.sql.candidate']
latest={}
for p in paths:
 for m in re.finditer(r'CREATE(?: OR REPLACE)? FUNCTION scheduler_private\.(\w+)\(.*?\$\$;',p.read_text(),re.S):latest[m[1]]=(p,m[0])
header="""-- INACTIVE forward Migration16 candidate. Requires unchanged approved M14 then M15.
-- Not in migrations; no activation, registrations or production authorization.
BEGIN;
SET LOCAL statement_timeout='2s';
SET LOCAL transaction_timeout='2s';
SET LOCAL lock_timeout='250ms';
DO $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND gate='closed' AND emergency_stop)
 OR EXISTS(SELECT 1 FROM public.monitor_runs WHERE physical_release_confirmed_at IS NULL)
 THEN RAISE EXCEPTION 'closed gate and trusted physical stop required' USING ERRCODE='55000'; END IF;
END $$;
ALTER TABLE public.scheduler_control DROP CONSTRAINT scheduler_control_execution_mode_check;
ALTER TABLE public.scheduler_control ADD CONSTRAINT scheduler_control_execution_mode_check CHECK(execution_mode IN ('fixture','private-isolated','private-approved'));
ALTER TABLE public.scheduler_control DROP CONSTRAINT scheduler_control_gate_check;
ALTER TABLE public.scheduler_control ADD CONSTRAINT scheduler_control_gate_check CHECK(gate IN ('closed','fixture','private-isolated','private-approved'));
"""
changes={}
p,s=latest['private_gate_allows'];assert s.count("('fixture','private-isolated')")==1
s=s.replace("('fixture','private-isolated')","('fixture','private-isolated','private-approved')")
old="IF p_gate='private-isolated' AND NOT EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND execution_mode='private-isolated')"
assert s.count(old)==1
s=s.replace(old,"IF p_gate IN ('private-isolated','private-approved') AND NOT EXISTS(SELECT 1 FROM public.scheduler_control WHERE control_id='deployment' AND execution_mode=p_gate)")
changes['private_gate_allows']=s
for name in ('authorize','execution_status'):
 p,s=latest[name]
 old="(gate='private-isolated' AND EXISTS(SELECT 1 FROM public.scheduler_control private_mode WHERE private_mode.control_id='deployment' AND private_mode.execution_mode='private-isolated'))"
 if name=='authorize':old=old.replace('(gate=', '(c.gate=')
 assert s.count(old)==1,(name,old)
 column='c.gate' if name=='authorize' else 'scoped.gate'
 # Explicit equivalence between deployment execution_mode and Gate; API owner
 # keeps its existing SELECT and obtains no scheduler-owner helper EXECUTE.
 new=f"({column} IN ('private-isolated','private-approved') AND EXISTS(SELECT 1 FROM public.scheduler_control private_mode WHERE private_mode.control_id='deployment' AND private_mode.execution_mode={column}))"
 changes[name]=s.replace(old,new)
p,s=latest['private_worker_ready']
assert s.count("d.gate='private-isolated'")==1
ready=s.replace('private_worker_ready(', 'approved_worker_ready(').replace("d.gate='private-isolated'","d.gate='private-approved'")
text=header
for name,s in changes.items():text+='\n-- Only mode predicate changes: '+name+'\n'+re.sub(r'^CREATE(?: OR REPLACE)? FUNCTION','CREATE OR REPLACE FUNCTION',s)+'\n'
text+='\n'+ready+"\nALTER FUNCTION scheduler_private.approved_worker_ready(uuid) OWNER TO devpilot_scheduler_owner;\nREVOKE ALL ON FUNCTION scheduler_private.approved_worker_ready(uuid) FROM PUBLIC;\nGRANT EXECUTE ON FUNCTION scheduler_private.approved_worker_ready(uuid) TO devpilot_scheduler_worker;\nCOMMIT;\n"
dest=ROOT/'deploy/targetadmission/approved-mode.sql.candidate'
manifest=dict(candidate_sha256=hashlib.sha256(text.encode()).hexdigest(),requires={str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in paths},new_roles=[],new_tables=[],new_columns=[],gate_value_added='private-approved',new_functions=['approved_worker_ready(uuid) [own boolean]'],grants=['EXECUTE approved_worker_ready(uuid) TO devpilot_scheduler_worker'],revokes=['PUBLIC ALL ON approved_worker_ready(uuid)'],table_column_grants=[],replacements=list(changes),production_activation=False)
out=json.dumps(manifest,indent=2)+'\n'
if '--check' in sys.argv:assert dest.read_text()==text and dest.with_name('permission-diff.json').read_text()==out
else:dest.write_text(text);dest.with_name('permission-diff.json').write_text(out)
print(manifest['candidate_sha256'])
