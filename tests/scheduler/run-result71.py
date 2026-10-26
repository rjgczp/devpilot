#!/usr/bin/env python3
"""Task7.1 real PG18/physical proof, normal and race; first failure stops."""
import argparse,json,subprocess,sys
from pathlib import Path
PLAN=[('TestResultMinimalPG',c) for c in ['allow-https','tls-invalid','rebinding','permit-missing','supervisor-killed']]+[('TestResultUnknownCommitPG','allow-https'),('TestResultActualLeaseExpiryPG','allow-https'),('TestResultAfterOwnerPatchPG','allow-https'),('TestResultTerminalUnknownPG','allow-https'),('TestResultControlRacePG','allow-https'),('TestResultPreflightPG','preflight-only')]
def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args();assert a.tools.is_absolute() and a.evidence.is_absolute();a.evidence.mkdir(parents=True,exist_ok=False);repo=Path(__file__).resolve().parents[2];runs=[]
 (a.evidence/'plan.json').write_text(json.dumps(dict(tests=PLAN,modes=['normal','race'],clusters=22,skip=False,retry=False),indent=2)+'\n')
 for mode in ['normal','race']:
  binary=a.tools/('result-pg-race.test' if mode=='race' else 'result-pg.test')
  info=subprocess.check_output(['go','version','-m',str(binary)],text=True);assert 'go1.24.3' in info and 'GOOS=linux' in info and 'resultfixture' in info;assert ('-race=true' in info)==(mode=='race')
  for test,case in PLAN:
   name=mode+'-'+test+'-'+case
   cmd=[sys.executable,'-B',str(repo/'tests/scheduler/result_fixture.py'),'--evidence',str(a.evidence/name),'--supervisor',str(a.tools/'networksupervisorfixture'),'--probe',str(a.tools/'schedulerprobefixture'),'--pg-binary',str(binary),'--mode',mode,'--case',case,'--test',test]
   record=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',name]
   for b in [binary,a.tools/'schedulerprobefixture',a.tools/'networksupervisorfixture',a.tools/'goose']:record+=['--binary',str(b)]
   code=subprocess.run(record+['--',*cmd],cwd=repo).returncode;runs.append(dict(mode=mode,test=test,case=case,exit_code=code));(a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=False,production_execution=False),indent=2)+'\n')
   if code:return code
 (a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=True,production_execution=False),indent=2)+'\n');return 0
if __name__=='__main__':raise SystemExit(main())
