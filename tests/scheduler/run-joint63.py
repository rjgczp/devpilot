#!/usr/bin/env python3
"""6.3 complete joint matrix, real Linux/PG18. First failure stops; no retries."""
import argparse,json,subprocess,sys
from pathlib import Path

CASES=['allow-https','tls-stop','tracer-stopped','tracer-crashed','supervisor-killed','stop-ack-lost','journal-collision','allow-http','permit-missing','permit-timeout','target-commit-unknown','supervisor-lost','rules-missing','old-fence','old-incarnation','cross-run','duplicate-envelope','cross-run-envelope','rebinding','ipv6-poison','tls-invalid','start-commit-unknown','input-commit-unknown']

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 assert a.tools.is_absolute() and a.evidence.is_absolute() and len(CASES)==len(set(CASES))==23
 a.evidence.mkdir(parents=True,exist_ok=False);repo=Path(__file__).resolve().parents[2]
 for name in ['joint-pg.test','joint-pg-race.test','schedulerprobefixture','networksupervisorfixture','goose']:
  info=subprocess.check_output(['go','version','-m',str(a.tools/name)],text=True);assert 'go1.24.3' in info and 'GOOS=linux' in info
  if '-race' in name:assert '-race=true' in info
 (a.evidence/'test-plan.json').write_text(json.dumps(dict(modes=['normal','race'],cases=CASES,clusters=46,retry=False,skip=False),indent=2)+'\n')
 # Positive NSS controls and the zero-socket relay startup regression run in a
 # separate network-none/caps0 container before any target fixture is created.
 nscd=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name','nscd-source','--',
       sys.executable,'-B',str(repo/'tests/scheduler/check-nscd-source.py'),'--evidence',str(a.evidence/'nscd-source')]
 status=subprocess.run(nscd,cwd=repo).returncode
 if status:return status
 denial=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name','denial-transport','--',
         sys.executable,'-B','-m','unittest','discover','-s','tests/scheduler','-p','test_joint_denial.py','-v']
 status=subprocess.run(denial,cwd=repo).returncode
 if status:return status
 runs=[]
 for mode in ['normal','race']:
  binary=a.tools/('joint-pg-race.test' if mode=='race' else 'joint-pg.test')
  for case in CASES:
   name=mode+'-'+case
   cmd=[sys.executable,'-B',str(repo/'tests/scheduler/joint_fixture.py'),'--evidence',str(a.evidence/name),'--supervisor',str(a.tools/'networksupervisorfixture'),'--probe',str(a.tools/'schedulerprobefixture'),'--pg-binary',str(binary),'--mode',mode,'--case',case]
   record=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',name]
   for b in [binary,a.tools/'schedulerprobefixture',a.tools/'networksupervisorfixture',a.tools/'goose']:record+=['--binary',str(b)]
   status=subprocess.run(record+['--',*cmd],cwd=repo).returncode
   runs.append(dict(mode=mode,case=case,command=cmd,exit_code=status))
   result=dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),runs=runs,complete=False,production_execution=False)
   (a.evidence/'result.json').write_text(json.dumps(result,indent=2)+'\n')
   if status:return status
 result['complete']=True;(a.evidence/'result.json').write_text(json.dumps(result,indent=2)+'\n');return 0
if __name__=='__main__':raise SystemExit(main())
