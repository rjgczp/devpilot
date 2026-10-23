import json,os,re,subprocess,sys,time,uuid
from pathlib import Path
repo=Path('/Users/sansea/Documents/ChatGPT/devpilot');out=Path('/private/tmp/devpilot-joint63-orchestration/snapshot-linux');out.mkdir(parents=True,exist_ok=False)
tools=Path('/private/tmp/devpilot-joint63-denial-tools');cli=Path('/private/tmp/devpilot-joint63-orchestration-tools/docker');assert cli.is_file()
probe=out/'probe-tools';probe.mkdir();(probe/'execution.test').write_bytes((tools/'execution.test').read_bytes());(probe/'execution.test').chmod(0o755)
image='postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336';runs=[]
for mode in ['normal','race']:
 label='devpilot-snapshot-'+uuid.uuid4().hex[:12];cid=None;dest=out/mode;dest.mkdir()
 try:
  create=['docker','run','-d','--name',label,'--label','devpilot.disposable.snapshot='+label,'--network','none','--memory=512m','--cpus=1','--pids-limit=128','-e','POSTGRES_USER=devpilot_fixture','-e','POSTGRES_PASSWORD=devpilot_fixture_only','-e','POSTGRES_DB=devpilot_registration_test',image,'postgres','-c','cluster_name=devpilot-snapshot-disposable','-c','log_lock_waits=on','-c','deadlock_timeout=100ms']
  cid=subprocess.check_output(create,text=True).strip();inspect=json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
  assert not inspect['HostConfig']['Privileged'] and not inspect['HostConfig']['CapAdd'] and not inspect['HostConfig']['PortBindings'] and set(inspect['NetworkSettings']['Networks'])=={'none'}
  (dest/'environment.json').write_text(json.dumps(dict(create_command=create,container=inspect),indent=2)+'\n')
  deadline=time.monotonic()+30
  while subprocess.run(['docker','exec',cid,'pg_isready','-h','127.0.0.1','-U','devpilot_fixture','-d','devpilot_registration_test'],capture_output=True).returncode:
   if time.monotonic()>deadline:raise RuntimeError('PG readiness exceeded30s')
   time.sleep(.2)
  base=['docker','run','--rm','--network','container:'+cid,'--cap-drop','ALL','--security-opt','no-new-privileges:true','-v',str(tools)+':/toolbin:ro','-v',str(repo)+':'+str(repo)+':ro','-w',str(repo),'-e','PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','-e','TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable']
  prepare=base+['--entrypoint','/toolbin/snapshot-schema','devpilot-scheduler-fixture']
  code=subprocess.run([sys.executable,'-B',str(repo/'tests/scheduler/record.py'),'--evidence',str(dest),'--name','schema6','--binary',str(tools/'goose'),'--binary',str(tools/'snapshot-schema'),'--',*prepare]).returncode
  if code:raise SystemExit(code)
  binary=tools/('snapshot-pg-race.test' if mode=='race' else 'snapshot-pg.test')
  cmd=base+['-v',str(cli)+':/usr/local/bin/docker:ro','-v','/var/run/docker.sock:/var/run/docker.sock','-v',str(probe)+':'+str(probe)+':ro','-v',str(dest)+':'+str(dest),'-e','EXECUTION_TEST_TOOLS='+str(probe),'-e','EXECUTION_PG_EVIDENCE='+str(dest/'network'),'--entrypoint','/toolbin/'+binary.name,'devpilot-scheduler-fixture','-test.run','^TestSystemSnapshot','-test.v','-test.count=1','-test.timeout=120s']
  # Docker CLI/socket belongs to this trusted test controller only. Original
  # Go test creates the unchanged network-none Probe without either mount.
  code=subprocess.run([sys.executable,'-B',str(repo/'tests/scheduler/record.py'),'--evidence',str(dest),'--name','tests','--binary',str(probe/'execution.test'),'--binary',str(binary),'--binary',str(cli),'--',*cmd]).returncode
  runs.append(dict(mode=mode,command=cmd,exit_code=code))
  (out/'result.json').write_text(json.dumps(dict(complete=False,runs=runs),indent=2)+'\n')
  if code:raise SystemExit(code)
  log=(dest/'tests.log').read_text();names=re.findall(r'^--- PASS: (TestSystemSnapshot\w+) ',log,re.M)
  assert len(names)==4 and len(set(names))==4 and '--- SKIP:' not in log,names
 finally:
  if cid:
   (dest/'postgres.log').write_bytes(subprocess.check_output(['docker','logs',cid],stderr=subprocess.STDOUT))
   current=json.loads(subprocess.check_output(['docker','inspect',cid]))[0];assert current['Config']['Labels'].get('devpilot.disposable.snapshot')==label
   subprocess.run(['docker','rm','-f','-v',cid],check=True,stdout=subprocess.DEVNULL)
(out/'result.json').write_text(json.dumps(dict(complete=True,runs=runs),indent=2)+'\n')
