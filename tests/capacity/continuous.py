#!/usr/bin/env python3
"""105 real no-upstream checks; explicit virtual eligibility intervals only."""
import argparse,base64,json,os,select,shutil,subprocess,sys,threading,time
from pathlib import Path
HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE.parent/'privateloop'))
from loop import LoopHarness
from network_fixture import IMAGE,LABEL

class CapacityHarness(LoopHarness):
 def __init__(self,*a):
  super().__init__(*a);self.worker_bootstrap=HERE/'worker_bootstrap.py';self.pg_deadline_seconds=2400
  self.expected_cycles=105;self.expected_gets=105;self.task_name='continuous-capacity';self.virtual_eligibility=True
  self.lock=threading.RLock();self.source_stop=threading.Event();self.sources=[];self.snapshots=[]
 def send(self,proc,value):
  with self.lock:return super().send(proc,value)
 def control_command(self,*a,**kw):
  cmd=super().control_command(*a,**kw)
  if kw.get('source'):cmd[-1]='-test.timeout=2400s'
  return cmd
 def public_catalog(self,selected):return dict(super().public_catalog(selected),case='capacity')
 def source_command(self,name,dsn,inc,kind):
  url=Path(self.private_files.name)/name;url.write_text(dsn+'\n');url.chmod(0o444)
  cmd=['docker','run','-i','--name',self.prefix+'-'+name,'--label',LABEL+'='+self.prefix,'--network',self.control,'--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','--memory=128m','--memory-swap=128m','--cpus=1','--pids-limit=64','--ulimit','nofile=256:256','-v',str(self.tools)+':/toolbin:ro','-v',str(url)+':/run/source-url:ro']
  if kind=='sampler':cmd+=['-v',self.volume+':/storage:ro','-v',self.logdir+':/logs-metadata:ro','-v',str(self.binding_file)+':/run/bindings:ro']
  cmd+=['--entrypoint','python3',IMAGE,'-S','-u','-c',(HERE/'source_bootstrap.py').read_text(),kind,inc]
  proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(self.dest/(name+'.stderr'),'wb'))
  self.processes.append(proc);self.sources.append(proc)
  cid=self.prefix+'-'+name;self.containers.append(cid);self.control_ids.add(cid)
  return proc
 def broker(self):
  pending={}
  try:
   while not self.source_stop.is_set():
    ready=select.select([self.sampler.stdout,self.watchdog.stdout],[],[],.1)[0]
    if self.sampler.stdout in ready:
     value=json.loads(self.read_bytes(self.sampler,time.monotonic()+2))
     if value.get('lost'):self.send(self.watchdog,dict(source_closed=True));break
     self.send(self.watchdog,value)
    if self.watchdog.stdout in ready:
     value=json.loads(self.read_bytes(self.watchdog,time.monotonic()+2))
     if value.get('lost'):break
     pending[value['index']]=value['pulse']
    for index,pulse in list(pending.items()):
     if index<len(self.workers):
      if self.workers[index].poll() is None:self.send(self.workers[index],dict(guard=pulse))
      del pending[index]
  except Exception as e:self.record('storage-broker-error',dict(type=type(e).__name__))
  finally:
   for w in self.workers:
    try:self.send(w,dict(storage_lost=True))
    except (BrokenPipeError,ValueError):pass
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='capacity-filesystems':
   info=json.loads(self.run(['docker','inspect',self.pg_id]))[0]
   mounts=[m for m in info['Mounts'] if m['Destination']=='/var/lib/postgresql'];assert len(mounts)==1 and mounts[0]['Type']=='volume'
   self.volume=mounts[0]['Name']
   base=['docker','run','--rm','--network','none','--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','-v',str(self.tools)+':/toolbin:ro','-v',self.volume+':/storage:ro']
   binding=json.loads(self.run(base+['--entrypoint','/toolbin/storagereporter',IMAGE,'--identify','/storage']))
   assert binding['device'] and binding['fsid']
   assert info['HostConfig']['LogConfig']['Type'] in ('json-file','local') and info['LogPath']
   self.logdir=str(Path(info['LogPath']).parent)
   logbase=base+['-v',self.logdir+':/logs-metadata:ro']
   logbinding=json.loads(self.run(logbase+['--entrypoint','/toolbin/storagereporter',IMAGE,'--identify','/logs-metadata']))
   self.bindings=[binding] if logbinding['device']==binding['device'] and logbinding['fsid']==binding['fsid'] else [binding,logbinding]
   topology=self.run(['docker','exec','--user','postgres',self.pg_id,'sh','-c','stat -Lc "%d %n" "$PGDATA" "$PGDATA/pg_wal" "$PGDATA/base"']).decode()
   for line in topology.splitlines():
    number=int(line.split()[0]);major=((number>>8)&0xfff)|((number>>32)&0xfffff000);minor=(number&0xff)|((number>>12)&0xffffff00)
    assert str(major)+':'+str(minor)==binding['device'],'PGDATA/WAL/default temp device mismatch'
   self.record('actual-device-map',dict(pgdata_wal_temp=topology,postgres_logs=logbinding,archive_and_external_tablespaces='must be rejected by SQL preflight'))
   self.binding_file=Path(self.private_files.name)/'bindings';self.binding_file.write_text(json.dumps(self.bindings));self.binding_file.chmod(0o444)
   script="import os,errno;\nfor p in ['/storage/18/docker/PG_VERSION','/logs-metadata/config.v2.json']:\n try:\n  fd=os.open(p,os.O_RDONLY);os.close(fd);raise SystemExit('protected content readable')\n except OSError as e:\n  assert e.errno==errno.EACCES,(e.errno,p)\nprint('EACCES: actual PG content and credential-bearing container config remain unreadable')"
   result=self.run(logbase+['--entrypoint','python3',IMAGE,'-S','-c',script]);self.record('reporter-metadata-boundary',dict(bindings=self.bindings,uid=65534,content_check=result.decode(),read_only=True,lower_layer_reservation_proven=False))
   return dict(frame=base64.b64encode(json.dumps([dict(device=b['device'],fsid=b['fsid']) for b in self.bindings]).encode()).decode())
  if op=='worker-capacity-start-sources':
   self.sampler=self.source_command('storage-sampler',payload['reporter_dsn'],payload['reporter_incarnation'],'sampler')
   self.watchdog=self.source_command('storage-watchdog',payload['reporter_dsn'],payload['reporter_incarnation'],'watchdog')
   self.thread=threading.Thread(target=self.broker,daemon=True);self.thread.start()
   root=Path(self.private_files.name);url=root/'retention-url';url.write_text(payload['controller_dsn']+'\n');url.chmod(0o444)
   name=self.prefix+'-retention'
   cmd=['docker','run','-i','--name',name,'--label',LABEL+'='+self.prefix,'--network',self.control,'--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','--memory=128m','--memory-swap=128m','--cpus=1','--pids-limit=64','-v',str(self.tools)+':/toolbin:ro','-v',str(url)+':/run/retention-url:ro','--entrypoint','/toolbin/retentioncontroller',IMAGE,'--run','--database-url-file','/run/retention-url','--incarnation',payload['controller_incarnation']]
   proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(self.dest/'retention.stderr','wb'));self.processes.append(proc);self.containers.append(name);self.control_ids.add(name)
   return dict(event='sources_started')
  if op=='worker-capacity-hold-unknown':
   assert not self.events('get');self.record('held-unknown',payload)
   self.event_offset=len(self.receiver_events);self.cycle+=1;self.life=None;self.request_seen=False
   return dict(event='unknown_held_without_release')
  if op=='worker-capacity-protected':
   self.record('protected-paths',payload);return dict(event='protected_paths_recorded')
  if op=='worker-capacity-snapshot':
   self.snapshots.append(payload);self.record('capacity-timeline',self.snapshots);return dict(event='recorded')
  return super().pg_operation(instance,op,payload,selected,mode)
 def archive_cycle(self):
  # Unique run-N audit/proof files already remain in the root. Copy only the
  # overwritten common witnesses, avoiding quadratic historical evidence copies.
  dest=self.dest/('cycle'+str(self.cycle));dest.mkdir(exist_ok=False)
  for name in ['committed-stop-bundle.json','probe-observation.json','joint-kernel-witness.json','both-target-rules-confirmed.json','kernel-request-after-dns.json']:
   p=self.dest/name
   if p.exists():shutil.copy2(p,dest/name)
  # All physical stop proofs were accepted before cycle-completed. Removing
  # those owned namespace containers now is cleanup, never a release proof.
  prefix=self.prefix+'-'+self.active.name+'-'
  for cid in list(self.containers):
   if cid.startswith(prefix):
    info=json.loads(self.run(['docker','inspect',cid]))[0];assert info['Config']['Labels'][LABEL]==self.prefix
    self.run(['docker','rm','-f',cid]);self.containers.remove(cid)
 def close(self):
  self.source_stop.set()
  if hasattr(self,'thread'):self.thread.join(timeout=2)
  for p in self.sources:
   if p.poll() is None:
    try:self.send(p,dict(op='stop'))
    except BrokenPipeError:pass
  super().close()

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 h=CapacityHarness(a.evidence.resolve(),a.tools.resolve(),'normal');status=1
 try:h.pg(h.tools/'private-loop-pg.test','normal');status=0
 finally:h.record('capacity-result',dict(exit_code=status,cycles=len(h.completed),real_interval=False,virtual_eligibility=True,internet=False,task83_physical_pressure_verified=False));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
