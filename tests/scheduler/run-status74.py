#!/usr/bin/env python3
"""Task7.4 actual HTTP projections over the accepted PG/Linux physical chain.

No fixture, network policy, timeout or assertion override. Exact inventory,
normal/race, no skips/retries, first failure stops.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

PLAN = ['TestExecutionStatusRealCompletedPG', 'TestExecutionStatusRealOldResultPG', 'TestExecutionStatusRealUnknownPG']

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--tools', type=Path, required=True)
    p.add_argument('--evidence', type=Path, required=True)
    a = p.parse_args()
    assert a.tools.is_absolute() and a.evidence.is_absolute()
    a.evidence.mkdir(parents=True, exist_ok=False)
    repo = Path(__file__).resolve().parents[2]
    (a.evidence/'plan.json').write_text(json.dumps(dict(tests=PLAN,modes=['normal','race'],skip=False,retry=False),indent=2)+'\n')
    runs=[]
    for mode in ['normal','race']:
        binary=a.tools/('recovery-pg-race.test' if mode=='race' else 'recovery-pg.test')
        info=subprocess.check_output(['go','version','-m',str(binary)],text=True)
        assert 'go1.24.3' in info and 'GOOS=linux' in info and 'recoveryfixture' in info
        assert ('-race=true' in info)==(mode=='race')
        cmd=['docker','run','--rm','--network','none','--cap-drop','ALL','--security-opt','no-new-privileges:true','-v',str(a.tools)+':/toolbin:ro','--entrypoint','/toolbin/'+binary.name,'devpilot-scheduler-fixture','-test.list','^TestExecutionStatusReal']
        listed=subprocess.run(cmd,capture_output=True)
        names=[x for x in listed.stdout.decode().splitlines() if x.startswith('TestExecutionStatusReal')]
        (a.evidence/('listing-'+mode+'.json')).write_text(json.dumps(dict(command=cmd,exit_code=listed.returncode,tests=names,binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest()),indent=2)+'\n')
        assert listed.returncode==0 and sorted(names)==sorted(PLAN)
        for test in PLAN:
            name=mode+'-'+test
            command=[sys.executable,'-B',str(repo/'tests/scheduler/recovery_fixture.py'),'--evidence',str(a.evidence/name),'--supervisor',str(a.tools/'networksupervisorfixture'),'--probe',str(a.tools/'schedulerprobefixture'),'--pg-binary',str(binary),'--mode',mode,'--case','allow-https','--test',test]
            receipt=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence/'receipts'),'--name',name]
            for b in [binary,a.tools/'schedulerprobefixture',a.tools/'networksupervisorfixture',a.tools/'goose']:receipt+=['--binary',str(b)]
            code=subprocess.run(receipt+['--',*command],cwd=repo).returncode
            runs.append(dict(mode=mode,test=test,exit_code=code))
            (a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=False,production_execution=False),indent=2)+'\n')
            if code:return code
    (a.evidence/'result.json').write_text(json.dumps(dict(runs=runs,complete=True,production_execution=False),indent=2)+'\n')
    return 0

if __name__=='__main__':raise SystemExit(main())
