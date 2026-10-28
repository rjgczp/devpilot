#!/usr/bin/env python3
"""Task7.3 real PG18/unknown quarantine recovery, normal and race; first failure stops."""
import argparse,hashlib,json,subprocess,sys
from pathlib import Path
PLAN=[
 ('TestRecoveryUnknownPG','allow-https'),('TestRecoveryNoPermitPG','permit-missing'),
 ('TestRecoveryTargetACKPG','target-commit-unknown'),('TestRecoveryStartACKPG','start-commit-unknown'),
 ('TestRecoveryInputACKPG','input-commit-unknown'),('TestRecoveryUnconfirmedPG','supervisor-killed'),
 ('TestRecoveryCommitACKPG','allow-https'),('TestRecoveryValidationPG','allow-https'),
 ('TestRecoveryPausePG','allow-https'),('TestRecoveryPatchPG','allow-https'),('TestRecoveryOwnerPG','allow-https'),
 ('TestRecoveryResultFirstPG','allow-https'),('TestRecoveryRecoveryFirstPG','allow-https'),
 ('TestRecoveryZombiePG','allow-https'),('TestRecoveryNextCyclePG','allow-https'),
 ('TestRecoveryPartitionPG','allow-https'),('TestRecoveryExpiryPG','allow-https'),('TestRecoveryEmergencyPG','tls-stop'),('TestRecoveryAmbiguousDenialPG','permit-missing'),('TestRecoveryQuotaDeniedPG','permit-missing'),
 ('TestRecoveryRecordedDenialPG','permit-missing'),('TestRecoveryStopOnlyPG','permit-missing')]

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args();assert a.tools.is_absolute() and a.evidence.is_absolute();a.evidence.mkdir(parents=True,exist_ok=False);repo=Path(__file__).resolve().parents[2];runs=[]
 (a.evidence/'plan.json').write_text(json.dumps(dict(tests=PLAN,modes=['normal','race'],clusters=len(PLAN)*2,skip=False,retry=False),indent=2)+'\n')
 for mode in ['normal','race']:
  binary=a.tools/('recovery-pg-race.test' if mode=='race' else 'recovery-pg.test')
  info=subprocess.check_output(['go','version','-m',str(binary)],text=True);assert 'go1.24.3' in info and 'GOOS=linux' in info and 'recoveryfixture' in info;assert ('-race=true' in info)==(mode=='race')
  listing=['docker','run','--rm','--network','none','--cap-drop','ALL','--security-opt','no-new-privileges:true','-v',str(a.tools)+':/toolbin:ro','--entrypoint','/toolbin/'+binary.name,'devpilot-scheduler-fixture','-test.list','^TestRecovery']
  listed=subprocess.run(listing,capture_output=True);(a.evidence/('listing-'+mode+'.log')).write_bytes(listed.stdout+listed.stderr)
  names=[x for x in listed.stdout.decode().splitlines() if x.startswith('TestRecovery')]
  (a.evidence/('listing-'+mode+'.json')).write_text(json.dumps(dict(command=listing,exit_code=listed.returncode,binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),tests=names),indent=2)+'\n')
  assert listed.returncode==0 and sorted(names)==sorted(t for t,c in PLAN),'exact recovery test manifest mismatch'
  for test,case in PLAN:
   name=mode+'-'+test+'-'+case
   cmd=[sys.executable,'-B',str(repo/'tests/scheduler/recovery_fixture.py'),'--evidence',str(a.evidence/name),'--supervisor',str(a.tools/'networksupervisorfixture'),'--probe',str(a.tools/'schedulerprobefixture'),'--pg-binary',str(binary),'--mode',mode,'--case',case,'--test',test]
   record=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',name]
   for b in [binary,a.tools/'schedulerprobefixture',a.tools/'networksupervisorfixture',a.tools/'goose']:record+=['--binary',str(b)]
   code=subprocess.run(record+['--',*cmd],cwd=repo).returncode;runs.append(dict(mode=mode,test=test,case=case,exit_code=code));(a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=False,production_execution=False),indent=2)+'\n')
   if code:return code
 (a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=True,production_execution=False),indent=2)+'\n');return 0
if __name__=='__main__':raise SystemExit(main())
