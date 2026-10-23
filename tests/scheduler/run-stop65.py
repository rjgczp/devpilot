#!/usr/bin/env python3
"""6.5-only independent termination; fail on missing dependency, no skip/retry."""
import argparse,hashlib,json,subprocess,sys
from pathlib import Path

p=argparse.ArgumentParser();p.add_argument('--tools',required=True,type=Path);p.add_argument('--evidence',required=True,type=Path);a=p.parse_args()
assert a.tools.is_absolute() and a.evidence.is_absolute()
a.evidence.mkdir(parents=True,exist_ok=False)
repo=Path(__file__).resolve().parents[2];runs=[]
for name in ['networksupervisorfixture','physical-stop-pg.test','physical-stop-pg-race.test','goose']:
 text=subprocess.check_output(['go','version','-m',str(a.tools/name)],text=True)
 assert 'go1.24.3' in text and 'GOOS=linux' in text
 if '-race' in name:assert '-race=true' in text
(a.evidence/'result.json').write_text('{}\n')
for name,binary in [('linux-termination',None),('pg-normal','physical-stop-pg.test'),('pg-race','physical-stop-pg-race.test')]:
 cmd=[sys.executable,'-B',str(repo/'tests/scheduler/termination_fixture.py'),'--supervisor',str(a.tools/'networksupervisorfixture'),'--evidence',str(a.evidence/name)]
 if binary:cmd+=['--pg-binary',str(a.tools/binary),'--mode','race' if name.endswith('race') else 'normal']
 record=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',name,'--binary',str(a.tools/'networksupervisorfixture')]
 if binary:record+=['--binary',str(a.tools/binary),'--binary',str(a.tools/'goose')]
 code=subprocess.run(record+['--',*cmd],cwd=repo).returncode
 runs.append(dict(name=name,command=cmd,exit_code=code))
 state=dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),task='6.5',runs=runs,complete=False,database_release_accepted=False,task63_joint_accepted=False)
 (a.evidence/'result.json').write_text(json.dumps(state,indent=2)+'\n')
 if code:raise SystemExit(code)
state['complete']=True;(a.evidence/'result.json').write_text(json.dumps(state,indent=2)+'\n')
