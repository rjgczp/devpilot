#!/usr/bin/env python3
"""Task5.4 requires actual Linux and PG18; every failure is fatal, no retry/skip."""
import argparse,json,subprocess,sys
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
assert a.tools.is_absolute() and a.evidence.is_absolute();a.evidence.mkdir(parents=True,exist_ok=False)
repo=Path(__file__).resolve().parents[2];runs=[]
for mode in ['normal','race']:
 binary=a.tools/('release-pg-race.test' if mode=='race' else 'release-pg.test')
 build=subprocess.check_output(['go','version','-m',str(binary)],text=True);assert 'go1.24.3' in build and 'GOOS=linux' in build
 if mode=='race':assert '-race=true' in build
 cmd=[sys.executable,'-B',str(repo/'tests/scheduler/release_fixture.py'),'--supervisor',str(a.tools/'networksupervisorfixture'),'--pg-binary',str(binary),'--mode',mode,'--evidence',str(a.evidence/mode)]
 code=subprocess.run([sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',mode,'--binary',str(binary),'--binary',str(a.tools/'goose'),'--binary',str(a.tools/'networksupervisorfixture'),'--',*cmd],cwd=repo).returncode
 runs.append(dict(mode=mode,command=cmd,exit_code=code))
 state=dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),task='5.4',runs=runs,complete=False,task63_joint_accepted=False)
 (a.evidence/'result.json').write_text(json.dumps(state,indent=2)+'\n')
 if code:raise SystemExit(code)
state['complete']=True;(a.evidence/'result.json').write_text(json.dumps(state,indent=2)+'\n')
