#!/usr/bin/env python3
"""Task7.3 known-Run recovery; accepted original kernel and physical stop chain."""
import argparse,json,time,threading,shutil
from pathlib import Path
from joint_fixture import JointHarness
class RecoveryHarness(JointHarness):
 def __init__(self,*args):
  super().__init__(*args);self.cycle=0;self.event_offset=0;self.active=None
 def pg_test_name(self):return '^'+self.recovery_test+'$'
 def pg_prepare(self,instance,payload):
  self.active=instance if self.cycle==0 else self.instance(self.cycle)
  return super().pg_prepare(self.active,payload)
 def archive_cycle(self):
  dest=self.dest/('cycle'+str(self.cycle));dest.mkdir(exist_ok=False)
  for source in list(self.dest.iterdir()):
   if source.is_file():shutil.copy2(source,dest/source.name)
 def events(self,kind='get'):
  self.receive_events();return [x for x in self.receiver_events[self.event_offset:] if x['kind']==kind]
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='next-cycle':
   self.receive_events();self.archive_cycle();self.event_offset=len(self.receiver_events);self.cycle+=1;self.life=None;self.request_seen=False
   return dict(event='fresh_cycle')
  if op=='partition':
   name=self.prefix+'-pg';self.run(['docker','pause',name])
   self.record('database-partition-begin',dict(at=time.time_ns(),name=name))
   def restore():
    self.run(['docker','unpause',name]);self.record('database-partition-end',dict(at=time.time_ns(),name=name))
   self.partition_timer=threading.Timer(3,restore);self.partition_timer.start()
   return dict(event='database_unavailable')
  if op=='partition-witness':
   self.partition_timer.join(timeout=4);assert not self.partition_timer.is_alive()
   return dict(event='database_restored')
  if op=='hold-zombie':
   self.run(['docker','kill','--signal','STOP',self.life.name])
   self.record('zombie-before-real-lease',self.exec(self.life.name,'ps','-e','-o','pid,ppid,stat,comm').decode())
   return dict(event='supervisor_frozen')
  if op=='zombie-witness':
   raw=self.exec(self.life.name,'ps','-e','-o','pid,ppid,stat,comm').decode();assert 'strace' in raw and 'python3' in raw
   self.record('zombie-after-real-lease',dict(processes=raw,at=time.time_ns(),receipt=False));return dict(event='still_live_quarantine')
  if op=='finish' and self.recovery_test=='TestRecoveryZombiePG':
   network=self.life.revoke(require_live=False);self.record('zombie-independent-network-revoke',network)
   self.run(['docker','kill','--signal','CONT',self.life.name])
   assert not (self.authority.path/(self.life.identity['run_id']+'.receipt')).exists()
   self.record('zombie-no-complete-proof',dict(receipt=False,capacity_released=False))
   return dict(event='unconfirmed_stop_quarantine')
  return super().pg_operation(self.active or instance,op,payload,selected,mode)
 def pg_completion(self,mode,status):
  self.receive_events();self.record('all-cycles-receiver-events',self.receiver_events)
  if self.cycle:self.archive_cycle()
  return dict(test=self.recovery_test,case=self.case,mode=mode,cycles=self.cycle+1,exit_code=status,production_execution=False)
def main():
 p=argparse.ArgumentParser();p.add_argument('--evidence',type=Path,required=True);p.add_argument('--supervisor',type=Path,required=True);p.add_argument('--probe',type=Path,required=True);p.add_argument('--pg-binary',type=Path,required=True);p.add_argument('--mode',choices=['normal','race'],required=True);p.add_argument('--case',required=True);p.add_argument('--test',required=True);a=p.parse_args()
 h=RecoveryHarness(a.evidence.resolve(),a.supervisor,a.probe,a.case,a.mode);h.recovery_test=a.test;status=1
 try:h.pg(a.pg_binary.resolve(),a.mode);status=0
 finally:h.record('recovery-result',dict(exit_code=status,test=a.test,case=a.case,mode=a.mode));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
