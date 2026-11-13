#!/usr/bin/env python3
"""Generate only an inactive forward candidate; never connect/apply/grant."""
import hashlib,json,re,sys
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
sources=list(sorted((ROOT/'migrations').glob('*.sql')))+[ROOT/'deploy/privateloop/private-mode.sql.candidate']
latest={}
for path in sources:
 for match in re.finditer(r'CREATE(?: OR REPLACE)? FUNCTION scheduler_private\.(\w+)\(.*?\$\$;',path.read_text().split('-- +goose Down')[0],re.S):latest[match[1]]=(path,match[0])
changes={}
def use(name,old,new,count=1):
 path,source=latest[name];body=changes.get(name,source)
 assert body.count(old)==count,(name,old,body.count(old));changes[name]=body.replace(old,new)

# Historical completed rows can retire without retiring the still-live schedule,
# budgets or target keys. Unknown and unreleased rows remain non-deletable.
use('run_capacity_accounting',"IF OLD.attempt_state IN ('leased','started')", "IF OLD.attempt_state IN ('leased','started','unknown')")
use('run_capacity_accounting',"s.last_attempt_run_id=OLD.run_id OR s.latest_result_run_id=OLD.run_id OR s.expires_at>clock_timestamp()\n   OR s.next_start_not_before>clock_timestamp() OR s.last_started_at>clock_timestamp()-interval '1 hour'", "s.last_attempt_run_id=OLD.run_id OR s.latest_result_run_id=OLD.run_id OR ((OLD.attempt_state<>'completed' OR OLD.result_commit_id IS NULL) AND (s.expires_at>clock_timestamp()\n   OR s.next_start_not_before>clock_timestamp() OR s.last_started_at>clock_timestamp()-interval '1 hour'))")
use('run_capacity_accounting',"AND (b.keep_until>clock_timestamp() OR b.not_before>clock_timestamp())", "AND (OLD.attempt_state<>'completed' OR OLD.result_commit_id IS NULL) AND (b.keep_until>clock_timestamp() OR b.not_before>clock_timestamp())")
use('cleanup_monitor',"pressure boolean;", "pressure boolean; rolling boolean;")
old=""" SELECT EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE (kind='global' AND terminal_count>=100000)
  OR (kind='workspace' AND budget_key=p_wid::text AND terminal_count>=10000)
  OR (kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND terminal_count>=100)) INTO pressure;
"""
use('cleanup_monitor',old,'')
condition=""" IF s.expires_at>n OR s.next_start_not_before>n OR s.last_started_at>n-interval '1 hour'
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND (keep_until>n OR not_before>n)) THEN"""
replacement=old.replace('terminal_count>=100))','terminal_count>=96))')+""" rolling:=COALESCE(s.expires_at>n OR s.next_start_not_before>n OR s.last_started_at>n-interval '1 hour',false)
 OR EXISTS(SELECT 1 FROM public.scheduler_budget_state WHERE kind='monitor' AND budget_key=p_wid::text||'/'||p_pid::text||'/'||p_mid::text AND (keep_until>n OR not_before>n));
 IF rolling AND NOT pressure THEN"""
use('cleanup_monitor',condition,replacement)
use('cleanup_monitor',"IF r.attempt_state IN ('leased','started')", "IF r.attempt_state IN ('leased','started','unknown')")
use('cleanup_monitor',"  SELECT * INTO q FROM public.scheduler_reservations", "  IF rolling AND (r.attempt_state<>'completed' OR r.result_commit_id IS NULL OR r.run_id=s.last_attempt_run_id OR r.run_id=s.latest_result_run_id) THEN CONTINUE; END IF;\n  SELECT * INTO q FROM public.scheduler_reservations")
use('cleanup_monitor',"  IF EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE", "  IF NOT rolling AND EXISTS(SELECT 1 FROM public.scheduler_budget_state b WHERE")
for name in ['claim','start_run','reserve_target']:
 gate='n' if name=='reserve_target' else 'gate_now'
 use(name,'c.disk_used_bytes<c.disk_budget_bytes*0.8','c.disk_used_bytes<c.disk_budget_bytes*0.8 AND scheduler_private.storage_admission('+gate+')')
use('private_worker_ready',"AND scheduler_private.private_gate_allows(d.gate,p_incarnation)","AND scheduler_private.private_gate_allows(d.gate,p_incarnation) AND scheduler_private.storage_admission(clock_timestamp())")
use('execution_status',"WHEN disk_used_bytes>=disk_budget_bytes*0.8 THEN", "WHEN disk_used_bytes>=disk_budget_bytes*0.8 OR NOT scheduler_private.storage_admission(now) THEN")
text=(ROOT/'deploy/capacity/storage-header.sql.candidate').read_text()
manifest=[]
for name,body in changes.items():
 path,source=latest[name]
 text+='\n-- Forward replacement: '+name+'\n'+re.sub(r'^CREATE(?: OR REPLACE)? FUNCTION','CREATE OR REPLACE FUNCTION',body)+'\n'
 manifest.append(dict(function=name,source=str(path.relative_to(ROOT)),source_sha256=hashlib.sha256(path.read_bytes()).hexdigest()))
text+='\n-- No registration, identity provisioning or activation.\nCOMMIT;\n'
p=ROOT/'deploy/capacity/continuous-capacity.sql.candidate'
if '--check' in sys.argv:assert p.read_text()==text
else:p.write_text(text)
report=json.dumps(dict(candidate_sha256=hashlib.sha256(text.encode()).hexdigest(),requires_private_mode_sha256=hashlib.sha256((ROOT/'deploy/privateloop/private-mode.sql.candidate').read_bytes()).hexdigest(),roles=['devpilot_storage_reporter NOLOGIN'],tables=['scheduler_private.storage_registration','scheduler_private.storage_state'],runtime_grants=['CONNECT current dedicated database TO devpilot_storage_reporter','USAGE scheduler_private TO devpilot_storage_reporter','EXECUTE storage_begin(uuid), storage_report(uuid,uuid,jsonb) TO devpilot_storage_reporter','EXECUTE storage_admission(timestamptz) TO devpilot_scheduler_api_owner'],worker_controller_supervisor_grants=[],existing_functions=manifest,production_activation=False),indent=2)+'\n'
m=ROOT/'deploy/capacity/permission-diff.json'
if '--check' in sys.argv:assert m.read_text()==report
else:m.write_text(report)
print(json.dumps(dict(path=str(p.relative_to(ROOT)),sha256=hashlib.sha256(text.encode()).hexdigest(),replaced_functions=list(changes))))
