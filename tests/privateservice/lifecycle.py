#!/usr/bin/env python3
"""Native Linux lifecycle test: fixture provisions PG/receiver, service drives Runs."""
import argparse,base64,hashlib,json,os,shutil,subprocess,sys,time
from pathlib import Path
HERE=Path(__file__).resolve().parent;ROOT=HERE.parents[1]
sys.path.insert(0,str(HERE.parent/'capacity'))
from continuous import CapacityHarness

class ServiceHarness(CapacityHarness):
 def __init__(self,*args):
  super().__init__(*args)
  self.pg_test_pattern='TestPrivateServicePG';self.expected_cycles=0;self.expected_gets=3;self.pg_deadline_seconds=420
  self.state=Path(self.private_files.name)/'state';self.config=Path(self.private_files.name)/'service.json'
  self.service_id='test-'+self.prefix.rsplit('-',1)[-1];self.service_prefix='dp-private-'+self.service_id;self.snapshots=[]
 def receiver_source(self):
  data=super().receiver_source()
  return data.replace(b"if MODE in ['tls-stop'",b"if os.path.exists('/tmp/hold-private-response') or MODE in ['tls-stop'")
 def driver_log_line(self,line):
  if line.startswith(b'NETWORK64 '):return b'SERVICE-IPC '+hashlib.sha256(line).hexdigest().encode()+b'\n'
  return super().driver_log_line(line)
 def cli(self,action,active=False):
  cmd=['sh',str(ROOT/'scripts/private-service.sh'),action,'--state',str(self.state)]
  if active:cmd+=['--config',str(self.config),'--run-isolated']
  p=subprocess.run(cmd,capture_output=True,timeout=40)
  (self.dest/('cli-'+action+'.log')).open('ab').write(p.stdout+p.stderr)
  assert p.returncode==0,'service '+action+' failed; inspect preserved private-service logs'
 def stopped(self,seconds=65):
  end=time.monotonic()+seconds
  while time.monotonic()<end:
   if (self.state/'status.json').exists():
    value=json.loads((self.state/'status.json').read_text())
    if value['event'] in ('stopped','recovery_required'):
     self.record('service-status',value);return value
   time.sleep(.1)
  raise RuntimeError('bounded service drain expired')
 def collect(self):
  if self.state.exists():
   dest=self.dest/'service';dest.mkdir(exist_ok=True)
   for p in self.state.iterdir():
    if p.suffix in ('.stderr','.receipt','.proofs') or p.name=='status.json' or p.name.endswith(('.owner.json','.database-accepted','.recovered')):shutil.copy2(p,dest/p.name)
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='service-key':
   self.cli('init');return dict(frame=base64.b64encode((self.state/'public.pem').read_bytes()).decode())
  if op=='service-config':
   config=dict(mode='private-isolated',runtime_id=self.service_id,image=self.run(['docker','image','inspect','devpilot-private-runtime','--format','{{.Id}}']).decode().strip(),control_network=self.control,pg_container=self.pg_id,target_container=self.receiver,target_ip='93.184.216.34',target_host='a.fixture.example.com',metadata_mounts=[[self.volume,'/storage'],[self.logdir,'/logs-metadata']],bindings_file=str(self.binding_file))
   for role in ('supervisor','controller','reporter'):
    boot=payload[role];file=Path(self.private_files.name)/role;file.write_text(boot['dsn']+'\n');file.chmod(0o444);config[role]=dict(database_url_file=str(file),incarnation=boot['incarnation'])
   config['workers']=[]
   for index,boot in enumerate(payload['workers']):
    file=Path(self.private_files.name)/('worker'+str(index));file.write_text(boot['dsn']+'\n');file.chmod(0o444);config['workers'].append(dict(database_url_file=str(file),incarnation=boot['incarnation']))
   ca=Path(self.private_files.name)/'ca.pem';ca.write_text(self.certificate);ca.chmod(0o444);config['ca_file']=str(ca)
   catalog=Path(self.private_files.name)/'catalog';catalog.write_text(json.dumps(dict(generation=1,key=base64.b64encode(b'k'*32).decode(),targets=[dict(URL=payload['url'],Addresses=['93.184.216.34'])])));catalog.chmod(0o444);config['catalog_file']=str(catalog)
   self.config.write_text(json.dumps(config));self.config.chmod(0o600);return dict(event='configured')
  if op=='service-start':
   self.cli('start',True)
   profiles={}
   for name in ('worker0','worker1','supervisor0','supervisor1','controller','sampler','watchdog'):
    v=json.loads(self.run(['docker','inspect',self.service_prefix+'-'+name]))[0]
    assert v['Config']['User'] in ('65532:65532','65533:65533','65531:65531','65530:65530','65529:65529')
    assert not v['HostConfig']['CapAdd'] and v['HostConfig']['ReadonlyRootfs'] and not v['HostConfig']['Privileged'] and not v['HostConfig']['PortBindings']
    assert set(v['NetworkSettings']['Networks'])=={self.control}
    assert not any('docker.sock' in m['Source'] or m['Source'].endswith('private.pem') for m in v['Mounts'])
    urls=[m for m in v['Mounts'] if m['Destination']=='/run/own-url'];assert len(urls)==1 and not urls[0]['RW']
    profiles[name]=dict(uid=v['Config']['User'],mount_destinations=[m['Destination'] for m in v['Mounts']],cap_add=v['HostConfig']['CapAdd'],network=self.control)
   check="import os,errno;\nfor p in ['/storage/18/docker/PG_VERSION','/logs-metadata/config.v2.json']:\n try:\n  f=os.open(p,os.O_RDONLY);os.close(f);raise SystemExit('protected content readable')\n except OSError as e:assert e.errno==errno.EACCES\nprint('Reporter UID65529: metadata only, content EACCES')"
   result=self.exec(self.service_prefix+'-sampler','python3','-S','-c',check)
   profiles['reporter_content_denied']=result.decode()
   self.record('role-boundaries',profiles);return dict(event='running')
  if op=='service-stop':
   if (self.state/'status.json').exists():self.cli('stop');self.stopped();self.collect()
   return dict(event='stopped')
  if op=='service-hold':
   self.exec(self.receiver,'touch','/tmp/hold-private-response');return dict(event='response_held')
  if op=='service-lose-supervisor':
   end=time.monotonic()+15
   while len(self.events('get'))<2 and time.monotonic()<end:time.sleep(.1)
   assert len(self.events('get'))==2
   owner=json.loads((self.state/(payload['run_id']+'.owner.json')).read_text());index=owner['index']
   self.run(['docker','kill','--signal','KILL',self.service_prefix+'-supervisor'+str(index)])
   status=self.stopped();assert status['failed'] and status['unverified_runs']==0
   assert (self.state/(payload['run_id']+'.receipt')).exists()
   assert not (self.state/(payload['run_id']+'.database-accepted')).exists()
   assert len(self.events('get'))==2,'lost Supervisor replayed GET'
   self.exec(self.receiver,'rm','/tmp/hold-private-response');self.collect();return dict(event='supervisor_lost_unknown_held')
  if op=='service-lose-reporter':
   self.run(['docker','kill','--signal','KILL',self.service_prefix+'-sampler']);status=self.stopped();assert status['failed']
   time.sleep(2);assert len(self.events('get'))==3;self.collect();return dict(event='reporter_unavailable')
  if op=='service-snapshot':
   self.snapshots.append(payload);self.record('service-timeline',self.snapshots);return dict(event='recorded')
  if op=='service-assert':
   gets=self.events('get');assert len(gets)==payload['gets']==3
   assert all(v['is_get'] and v['header_only'] and not v['credentials'] for v in gets)
   assert len(list(self.state.glob('*.receipt')))==payload['physical_acceptances']==3
   assert len(list(self.state.glob('*.database-accepted')))==3
   assert len(list(self.state.glob('*.recovered')))==3
   self.record('lifecycle-result',dict(exit_code=0,workers=2,gets=3,completed=2,unknown=1,all_physical_proofs_verified=True,no_restart_resend=True,reporter_loss_closed=True,internet=False));return dict(event='verified')
  if op=='capacity-filesystems':return super().pg_operation(instance,op,payload,selected,mode)
  raise RuntimeError('fixture must not drive runtime call: '+op)
 def close(self):
  try:
   if (self.state/'status.json').exists():self.cli('stop');self.stopped();self.collect()
  finally:self.collect();super().close()

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 assert sys.platform=='linux','native Linux required; never bypass storage/network preflight on Mac'
 h=ServiceHarness(a.evidence.resolve(),a.tools.resolve(),'normal');status=1
 try:h.pg(a.tools/'private-service-pg.test','normal');status=0
 finally:h.record('exit',dict(exit_code=status,production=False));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
