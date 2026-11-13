#!/usr/bin/env python3
"""Actual private Worker/Probe, private Gate, original signed physical stop."""
import argparse,base64,json,os,select,subprocess,sys,tempfile
from pathlib import Path
HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE.parent/'scheduler'));sys.path.insert(0,str(HERE.parent/'privateprobe'))
from periodic91 import PeriodicHarness
from lifecycle import PrivateHarness
from network_fixture import IMAGE,LABEL

class LoopHarness(PeriodicHarness):
 private_sources=PrivateHarness.private_sources
 network_config=PrivateHarness.network_config
 def __init__(self,dest,tools,mode):
  super().__init__(dest,tools,mode)
  self.binary=(tools/'privatenetwork').resolve();self.probe=(tools/'privateprobe').resolve()
  self.pg_test_pattern='TestPrivateLoopPG';self.workers=[];self.mode=mode
  self.dead=set();self.pending=[];self.boot_commands=[];self.exit_codes={}
  self.private_files=tempfile.TemporaryDirectory(prefix='devpilot-private-loop-')
  os.chmod(self.private_files.name,0o700)
 def control_command(self,*a,**kw):
  cmd=super().control_command(*a,**kw)
  if kw.get('source'):cmd[-1]='-test.timeout=540s'
  return cmd
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='worker-private-start':
   assert len(payload)==2 and not self.workers
   for index,boot in enumerate(payload):
    root=Path(self.private_files.name);url=root/('worker'+str(index));url.write_text(boot['DSN']+'\n');url.chmod(0o444)
    catalog=root/('catalog'+str(index));catalog.write_text(json.dumps(dict(generation=1,key=base64.b64encode(b'k'*32).decode(),targets=[dict(URL='https://a.fixture.example.com/fixture/private-path-SENTINEL',Addresses=[boot['IP']])])));catalog.chmod(0o444)
    name=self.prefix+'-private-worker'+str(index)
    cmd=['docker','run','-i','--name',name,'--label',LABEL+'='+self.prefix,'--network',self.control,'--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','--memory=256m','--memory-swap=256m','--cpus=1','--pids-limit=128','--ulimit','nofile=256:256','-v',str(self.tools)+':/toolbin:ro','-v',str(url)+':/run/worker-url:ro','-v',str(catalog)+':/run/worker-catalog:ro','--entrypoint','python3',IMAGE,'-S','-u','-c',getattr(self,'worker_prelude','')+getattr(self,'worker_bootstrap',HERE/'worker_bootstrap.py').read_text(),boot['Incarnation']]
    self.boot_commands.append(cmd)
    proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(self.dest/('private-worker'+str(index)+'.stderr'),'wb'))
    self.processes.append(proc);self.workers.append(proc);self.containers.append(name);self.control_ids.add(name)
    first=json.loads(self.read_bytes(proc,__import__('time').monotonic()+15))
    assert first['kind']=='request' and first['request']['op']=='hello','native Worker must reach its bounded Supervisor handshake'
    self.pending.append((index,first))
    self.record('private-worker'+str(index)+'-container',json.loads(self.run(['docker','inspect',name]))[0])
   self.assert_wires();return dict(event='workers_started')
  if op=='worker-private-poll':
   if self.pending:
    index,value=self.pending.pop(0)
    return dict(worker=dict(op='private-'+value['kind'],payload=dict(index=index,**value)))
   ready=select.select([w.stdout for i,w in enumerate(self.workers) if i not in self.dead],[],[],.2)[0]
   for index,w in enumerate(self.workers):
    if w.stdout in ready:
     raw=self.read_bytes(w,__import__('time').monotonic()+3);value=json.loads(raw)
     if value['kind']=='exit':self.dead.add(index);self.exit_codes[index]=value['exit_code']
     if value['kind']=='event':self.record('worker-event-'+str(index)+'-'+str(len(self.worker_log)),value);self.worker_log.append(dict(worker=index,event=value['event']))
     return dict(worker=dict(op='private-'+value['kind'],payload=dict(index=index,**value)))
   return dict(event='idle')
  if op=='worker-private-restart':
   index=payload['index'];assert index in self.dead and self.exit_codes[index]!=0
   assert self.workers[index].wait(timeout=3)==0 # Relay reports native exit separately.
   name=self.prefix+'-private-worker'+str(index)
   self.run(['docker','rm',name])
   proc=subprocess.Popen(self.boot_commands[index],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(self.dest/('private-worker'+str(index)+'-restart.stderr'),'wb'))
   self.processes.append(proc);self.workers[index]=proc
   first=json.loads(self.read_bytes(proc,__import__('time').monotonic()+15))
   assert first['kind']=='request' and first['request']['op']=='hello'
   self.pending.append((index,first));self.dead.remove(index);self.assert_wires()
   return dict(event='worker_restarted')
  if op=='worker-private-reply':
   self.send(self.workers[payload['index']],dict(reply=payload['reply']));return dict(event='reply_sent')
  if op=='worker-kill-supervisor':
   self.run(['docker','kill','--signal','KILL',self.life.name]);assert self.life.proc.wait(timeout=3)!=0
   self.life.revoke(require_live=False)
   return dict(event='supervisor_lost')
  if op=='worker-negative-complete':
   self.receive_events()
   current=self.events('get')
   assert len(current)==(1 if payload['case']=='lost' else 0),'unexpected GET or restart resend'
   self.record('negative-scenario',dict(payload,receiver_gets_in_current_run=len(current)))
   return dict(event='negative_recorded')
  if op=='worker-private-stop':
   for w in self.workers:
    if w.poll() is None:
     try:self.send(w,dict(op='stop'))
     except BrokenPipeError:pass
   for w in self.workers:w.wait(timeout=8)
   return dict(event='workers_stopped')
  return super().pg_operation(instance,op,payload,selected,mode)
 def close(self):
  try:super().close()
  finally:self.private_files.cleanup()

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);p.add_argument('--case',choices=['cycles','expiry','pause','lost'],default='cycles');p.add_argument('--mode',choices=['normal','race'],default='normal');a=p.parse_args()
 h=LoopHarness(a.evidence.resolve(),a.tools.resolve(),a.mode);h.scenario=a.case;h.task_name='private-loop';h.expected_cycles=3 if a.case=='cycles' else 1 if a.case=='lost' else 0;h.expected_gets=2 if a.case=='lost' else h.expected_cycles
 original=h.public_catalog
 def catalog(selected):return dict(original(selected),case=a.case)
 h.public_catalog=catalog
 status=1
 try:h.pg(h.tools/'private-loop-pg.test',a.mode);status=0
 finally:h.record('private-loop-result',dict(exit_code=status,case=a.case,private_gate=True,storage_capacity_verified=False,production_execution=False));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
