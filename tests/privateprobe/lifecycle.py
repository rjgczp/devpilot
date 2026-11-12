#!/usr/bin/env python3
"""Actual private binaries on the existing no-upstream PG18/physical fixture.
The old DB fixture gate is test evidence only; private Worker stays closed.
"""
import argparse,json,sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scheduler'))
from joint_fixture import JointHarness
from network_fixture import ALIASES4, ALIASES6

class PrivateHarness(JointHarness):
 def __init__(self,*a,**kw):
  super().__init__(*a,**kw)
  self.lost_after_ack=self.case=='lifetime-lost'
  if self.lost_after_ack:self.case='rules-missing' # Existing nil-Result assertion.
 def install_fast(self,instance,payload):
  if not self.lost_after_ack:return super().install_fast(instance,payload)
  self.case='allow-https'
  try:return super().install_fast(instance,payload)
  finally:self.case='rules-missing'
 def pg_operation(self,instance,op,payload,selected,mode):
  if self.lost_after_ack and op=='reply':
   self.life.send(dict(op='lose-lifetime',lineage=self.life.identity))
   assert self.readline(self.life.proc)==dict(event='lifetime_lost')
  return super().pg_operation(instance,op,payload,selected,mode)
 def pg_prepare(self,instance,payload):
  answer=super().pg_prepare(instance,payload)
  actions=[dict(ip=ip,port=443,tls=True) for ip in ALIASES4+ALIASES6]
  before=len(self.events('accept'))
  result=instance.actor(actions,'private-default-deny')
  assert all(not a['connected'] for a in result['actions'])
  assert len(self.events('accept'))==before,'default-deny leaked a target connect'
  return answer
 def private_sources(self,bootstrap,lifecycle):
  # Only PID1 retains the writer. The traced relay and Probe get a read end.
  lifecycle=lifecycle.replace("cfg=message();assert cfg['op']=='boot'", "life_r,life_w=os.pipe();os.dup2(life_r,19,inheritable=True);os.close(life_r)\ncfg=message();assert cfg['op']=='boot'")
  lifecycle=lifecycle.replace("elif op=='inspect':","elif op=='lose-lifetime':os.close(life_w);emit(dict(event='lifetime_lost'))\n  elif op=='inspect':")
  lifecycle=lifecycle.replace('close_fds=True,preexec_fn=drop','close_fds=True,pass_fds=(19,),preexec_fn=drop')
  # The source descriptor is duplicated before the relay creates its four pipes.
  bootstrap=bootstrap.replace('original=[]', 'life=fcntl.fcntl(19,fcntl.F_DUPFD_CLOEXEC,30);os.close(19)\noriginal=[]')
  bootstrap=bootstrap.replace('child=subprocess.Popen(', 'os.dup2(life,7,inheritable=True);os.close(life)\nchild=subprocess.Popen(')
  bootstrap=bootstrap.replace('pass_fds=(3,4,5,6)', 'pass_fds=(3,4,5,6,7)').replace('for fd in [3,4,5,6,er,ar,qw,rr]', 'for fd in [3,4,5,6,7,er,ar,qw,rr]')
  return bootstrap,lifecycle
 def network_config(self,cfg):
  return dict(cfg,target='93.184.216.34' if self.mode=='normal' else '2001:4860:ffff::34',denied=['11.0.0.0/8','2600:dead::/32'])

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);p.add_argument('--case',required=True);p.add_argument('--mode',choices=['normal','race'],required=True);a=p.parse_args()
 h=PrivateHarness(a.evidence.resolve(),a.tools/'privatenetwork',a.tools/'privateprobe',a.case,a.mode);status=1
 try:h.pg(a.tools/('periodic-pg.test' if a.mode=='normal' else 'periodic-pg-race.test'),a.mode);status=0
 finally:h.record('private-runtime-result',dict(exit_code=status,case=a.case,mode=a.mode,public_execution=False));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
