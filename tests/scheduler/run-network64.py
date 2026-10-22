#!/usr/bin/env python3
"""Task6.4-only acceptance. No retries/skips, no production probe or slot release."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--tools',required=True,type=Path)
    parser.add_argument('--evidence',required=True,type=Path)
    args=parser.parse_args()
    repo=Path(__file__).resolve().parents[2]
    tools=args.tools.resolve();dest=args.evidence.resolve()
    assert args.evidence.is_absolute() and args.tools.is_absolute()
    dest.mkdir(parents=True,exist_ok=False)
    names=['networksupervisorfixture','supervisorfixture.test','supervisorfixture-race.test','network64-pg.test','network64-pg-race.test','goose']
    binary_info={}
    for name in names:
        binary=tools/name
        text=subprocess.check_output(['go','version','-m',str(binary)],text=True)
        assert 'go1.24.3' in text and 'GOOS=linux' in text,name
        if '-race' in name:assert '-race=true' in text,name
        binary_info[name]=dict(sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),build=text)
    assert subprocess.check_output(['go','version'],text=True).startswith('go version go1.24.3 ')
    image=json.loads(subprocess.check_output(['docker','image','inspect','devpilot-scheduler-fixture']))[0]
    assert image['Id']
    (dest/'toolchain.json').write_text(json.dumps(dict(binaries=binary_info,image_id=image['Id']),indent=2)+'\n')
    runs=[]
    def recorded(name,command,binaries=()):
        cmd=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(dest/'receipts'),'--name',name]
        for binary in binaries:cmd+=['--binary',str(binary)]
        cmd+=['--',*command]
        status=subprocess.run(cmd,cwd=repo).returncode
        runs.append(dict(name=name,command=command,exit_code=status))
        (dest/'result.json').write_text(json.dumps(dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),runs=runs,complete=False),indent=2)+'\n')
        if status:raise SystemExit(status)
    for name in ['supervisorfixture.test','supervisorfixture-race.test']:
        recorded(name.replace('.','-'),['docker','run','--rm','--network','none','--cap-drop','ALL','-v',str(tools/name)+':/suite:ro',
                                      '--entrypoint','/suite','devpilot-scheduler-fixture','-test.v','-test.timeout=30s'],[tools/name])
    recorded('network-adversarial',[sys.executable,str(repo/'tests/scheduler/network_fixture.py'),'--evidence',str(dest/'network'),
                                   '--supervisor',str(tools/'networksupervisorfixture')],[tools/'networksupervisorfixture'])
    for mode,binary in [('normal','network64-pg.test'),('race','network64-pg-race.test')]:
        recorded('pg-'+mode,[sys.executable,str(repo/'tests/scheduler/network_fixture.py'),'--evidence',str(dest/('pg-'+mode)),
                             '--supervisor',str(tools/'networksupervisorfixture'),'--pg-binary',str(tools/binary),'--mode',mode],
                            [tools/'networksupervisorfixture',tools/binary,tools/'goose'])
    (dest/'result.json').write_text(json.dumps(dict(head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip(),runs=runs,complete=True,
                                                   task='6.4',physical_stop_accepted=False,quarantine_release_accepted=False,task63_joint_accepted=False),indent=2)+'\n')


if __name__=='__main__':
    main()
