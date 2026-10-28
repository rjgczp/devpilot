import argparse,hashlib,json,re
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--source',type=Path,required=True);p.add_argument('--result-count',type=int,default=24);p.add_argument('--kind',choices=['security','integration','verify'],required=True);p.add_argument('--out',type=Path,required=True);a=p.parse_args();frozen=json.loads(a.source.read_text());root=a.root

def load(path):return json.loads(path.read_text())
def checked(r,context):
 assert r['exit_code']==0,(context,r['exit_code'])
 assert r['head']==frozen['head'],(context,r['head'])
 assert not r['dirty'],(context,'dirty')
 assert r['source_inventory_sha256']==frozen['source_inventory_sha256'],(context,'inventory')
 assert r['sources']==frozen['sources'],(context,'sources')
rec=[]
for path in root.rglob('*.json'):
 try:r=load(path)
 except (ValueError,UnicodeError):continue
 if isinstance(r,dict) and {'source_inventory_sha256','sources','command','exit_code','head'}<=r.keys():
  checked(r,str(path));log=path.with_suffix('.log')
  if log.exists():assert hashlib.sha256(log.read_bytes()).hexdigest()==r['log_sha256'],str(log)
  rec.append(str(path.relative_to(root)))
assert rec
result=dict(head=frozen['head'],source_inventory_sha256=frozen['source_inventory_sha256'],kind=a.kind,successful_source_bound_receipts=len(rec),receipts=rec)
if a.kind=='security':
 joint=load(root/'joint63-evidence/result.json');assert joint['complete'] and len(joint['runs'])==46 and all(r['exit_code']==0 for r in joint['runs'])
 new=load(root/'result71-evidence/result.json');assert new['complete'] and len(new['runs'])==a.result_count and all(r['exit_code']==0 for r in new['runs'])
 for s in ['network64','stop65','release54']:
  r=load(root/(s+'-evidence/result.json'));assert r['complete'] and all(x['exit_code']==0 for x in r['runs'])
 budgets={}
 for mode in ['normal','race']:
  candidates=list((root/f'budget-evidence/{mode}').glob('*coverage.json'))
  assert len(candidates)==1,candidates
  r=load(candidates[0]);assert r['complete'] and len(r['runs'])==23 and all(x['exit_code']==0 for x in r['runs']);budgets[mode]=len(r['runs'])
 snapshots={}
 for mode in ['normal','race']:
  s=(root/f'execution-meta/snapshot-{mode}.log').read_text();assert '--- FAIL:' not in s and '--- SKIP:' not in s
  passed=re.findall(r'^--- PASS: (TestSystemSnapshot\w+)',s,re.M);assert len(set(passed))==4; snapshots[mode]=passed
 summary=load(root/'execution-network/summary.json');assert len(summary)==81 and all(r['child_exit']==0 and r['audit_exit']==int(r['case'].startswith('bypass-')) for r in summary)
 rows=[]
 for mode in ['normal','race']:
  for test,case in [('TestResultMinimalPG','allow-https'),('TestResultMinimalPG','permit-missing'),('TestResultMinimalPG','supervisor-killed'),('TestResultUnknownCommitPG','allow-https'),('TestResultAfterOwnerPatchPG','allow-https'),('TestResultActualLeaseExpiryPG','allow-https')]:
   base=root/f'result71-evidence/{mode}-{test}-{case}';ledger=load(base/'postgres-final-ledger.log');assert len(ledger['runs'])==1
   r=ledger['runs'][0];q=ledger['reservations'][0];assert all(r[k]==q[k] for k in ['workspace_id','project_id','monitor_id','run_id'])
   g=next(x for x in ledger['budgets'] if x['kind']=='global')
   row=dict(mode=mode,test=test,case=case,attempt_state=r['attempt_state'],success=r['success'],http_status=r['http_status'],stop_reason=r['stop_reason'],global_in_use=g['in_use'])
   if case=='supervisor-killed':
    assert q['reservation_state']=='quarantined' and g['in_use']==1 and r['stop_reason']=='worker_lost' and r['success'] is None and r['physical_release_confirmed_at'] is None and q['termination_receipt'] is None
   else:
    b=load(base/'committed-stop-bundle.json');l=b['process']['lineage'];assert b['process']['remaining_pids']==[] and b['process']['pids'] and all(p['pidfd_exited'] for p in b['process']['pids'])
    for key in ['workspace_id','project_id','monitor_id','run_id','configuration_version','activation_generation','scheduler_epoch','fencing_token']:assert str(r[key])==l[key]
    assert l['namespace_incarnation']==q['namespace_incarnation'] and l['supervisor_incarnation']==q['supervisor_incarnation']
    assert b['retired']['lineage']==l and all(x['lineage']==l and x['conntrack_empty'] and x['link_removed'] for x in b['network'])
    assert q['reservation_state']=='released' and g['in_use']==0 and r['physical_release_confirmed_at']==q['released_at'] and q['termination_receipt_id']
    w=load(base/'joint-kernel-witness.json');assert w['physical_stop'] and w['get_count']==int(case=='allow-https') and w['target_connect_syscalls']==int(case=='allow-https')
    if case=='permit-missing':assert r['success'] is None and r['http_status'] is None and r['stop_reason']=='egress_unavailable'
    elif test=='TestResultActualLeaseExpiryPG':assert r['attempt_state']=='started' and r['success'] is None
    else:assert r['attempt_state']=='completed' and r['success'] is True and r['http_status']==200 and r['failure_code'] is None and r['result_commit_id']
    row.update(witness=w,pidfd_exits=len(b['process']['pids']),full_lineage_matches=True)
   assert not any(s in json.dumps(r) for s in ['private-path-SENTINEL','a.fixture.example.com','SECRET_RESULT_SENTINEL','Cookie','password','raw_error'])
   rows.append(row)
 result.update(joint63=46,result71=a.result_count,budget=budgets,snapshots=snapshots,network_scenarios=81,physical_result_binding=rows)
if a.kind=='integration':
 for mode in ['normal','race']:
  r=load(root/f'integration-evidence/{mode}/result.json');assert r['head']==frozen['head'] and r['complete'] and r['pg_test_count']==147 and sum(x['count'] for x in r['groups'])==147
  plan=load(root/f'integration-evidence/{mode}/plan.json');expected=[x for g in plan['groups'] for x in g['tests']];passed=[]
  for g in r['groups']:
   events=[json.loads(l) for l in (root/f'integration-evidence/{mode}/{g["name"]}.log').read_text().splitlines()]
   assert not any(e['Action']=='skip' for e in events)
   passed += [e['Test'] for e in events if e['Action']=='pass' and e.get('Test') and '/' not in e['Test']]
  assert len(passed)==len(set(passed))==147 and set(passed)==set(expected)
 tests=root/'monitor-network-evidence'
 audit=[p.read_text() for p in tests.glob('*.audit.txt') if p.stem!='control.audit'];assert audit
 for text in audit:assert 'blocked_attempts=0' in text or 'violations=0' in text,text[:100]
 result['pg_tests_each_mode']=147
 assert (tests/'real.exit.txt').read_text().strip()=='child_exit=0 audit_exit=0'
 assert (tests/'control.exit.txt').read_text().strip()=='child_exit=0 audit_exit=1'
 result['api_boundary']=(tests/'real.audit.txt').read_text().strip()
if a.kind=='verify':
 assert any('unit.json' in p for p in rec) and any('race.json' in p for p in rec)
a.out.parent.mkdir(parents=True,exist_ok=True);a.out.write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({k:v for k,v in result.items() if k not in ['receipts','physical_result_binding','snapshots']},indent=2))
