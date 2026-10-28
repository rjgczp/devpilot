#!/usr/bin/env python3
"""Real input-only Linux process boundary; no source/config mounts or network."""
import argparse,json,subprocess,sys,uuid
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--binary',required=True,type=Path);p.add_argument('--probe',required=True,type=Path);p.add_argument('--evidence',required=True,type=Path);p.add_argument('--name',required=True);a=p.parse_args()
repo=Path(__file__).resolve().parents[2];a.evidence.mkdir(parents=True,exist_ok=True)
for binary in [a.binary,a.probe]:assert binary.resolve().parent==Path('/private/tmp/devpilot-scheduler-tools')
name='devpilot-input-'+uuid.uuid4().hex[:12]
create=['docker','create','--name',name,'--label','devpilot.disposable.input='+name,'--network','none','--read-only','--memory=128m','--memory-swap=128m','--cpus=1','--pids-limit=128','--ulimit','nofile=256:256','--ulimit','core=0','--cap-drop','ALL','--cap-add','SETUID','--cap-add','SETGID','--cap-add','SETPCAP','--security-opt','no-new-privileges','-v',f'{a.probe.resolve()}:/testbin/cp4-probe:ro','-v',f'{a.binary.resolve()}:/testbin/process.test:ro','-e','DB_PASSWORD=DB_PASSWORD_SENTINEL','-e','COOKIE=COOKIE_SENTINEL','-e','AWS_SECRET_ACCESS_KEY=CLOUD_SECRET_SENTINEL','--entrypoint','/testbin/process.test','golang:1.24.3-bookworm','-test.v','-test.failfast','-test.timeout=30s']
cid=subprocess.check_output(create,text=True).strip();status=1
try:
 inspect=json.loads(subprocess.check_output(['docker','inspect',cid]))[0];h=inspect['HostConfig']
 assert not h['Privileged'] and h['ReadonlyRootfs'] and h['Memory']==134217728 and h['MemorySwap']==134217728 and h['NanoCpus']==1000000000 and h['PidsLimit']==128
 assert h['NetworkMode']=='none' and not h['PortBindings'] and set(h['CapAdd'])=={'CAP_SETUID','CAP_SETGID','CAP_SETPCAP'}
 assert len(inspect['Mounts'])==2 and all(not m['RW'] and m['Destination'].startswith('/testbin/') for m in inspect['Mounts'])
 (a.evidence/(a.name+'-create.json')).write_text(json.dumps({'command':create,'container':inspect},indent=2)+'\n')
 cmd=[sys.executable,str(repo/'tests/scheduler/record.py'),'--evidence',str(a.evidence),'--name',a.name,'--binary',str(a.binary),'--binary',str(a.probe),'--','docker','start','-a',cid]
 status=subprocess.run(cmd,cwd=repo).returncode
 final=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
 (a.evidence/(a.name+'-final-inspect.json')).write_text(json.dumps(final,indent=2)+'\n')
 assert not final['State']['OOMKilled'] and final['State']['ExitCode']==0
finally:
 owned=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
 assert owned['Config']['Labels'].get('devpilot.disposable.input')==name
 subprocess.run(['docker','rm','-f','-v',cid],check=True,stdout=subprocess.DEVNULL)
sys.exit(status)
