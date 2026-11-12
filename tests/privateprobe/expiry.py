#!/usr/bin/env python3
"""Private policy expiry after real manager SIGKILL; no DB/Run release claim."""
import argparse,datetime,json,sys,time
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scheduler'))
from network_fixture import Harness,ALIASES4,ALIASES6,now_string

class PrivatePolicy(Harness):
 def network_config(self,cfg):return dict(cfg,target=self.target,denied=['11.0.0.0/8','2600:dead::/32'])

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 h=PrivatePolicy(a.evidence,a.tools/'privatenetwork');status=1
 try:
  h.setup_receiver()
  for index,target in enumerate(['93.184.216.34','2001:4860:ffff::34']):
   h.target=target;instance=h.instance(index);instance.boot(deadline=now_string(25))
   # Explicit standalone, synthetic management proof: kernel test only, not a
   # claimed committed scheduler reservation. PG authorization is tested apart.
   before=len(h.events('accept'));instance.install(ip=target,port=443,seconds=8)
   bad=instance.actor([dict(ip=ip,port=443,tls=True) for ip in ALIASES4+ALIASES6],'private-installed-denials')
   assert not any(x['connected'] for x in bad['actions']) and len(h.events('accept'))==before
   # Both managers are killed, not gracefully closed. Their actual kernel TTLs
   # must expire without a userspace cleanup/renewal process.
   for cid in (instance.run,instance.gateway):
    h.exec(cid,'python3','-c',"import os,signal,pathlib\nfor p in pathlib.Path('/proc').iterdir():\n if p.name.isdigit():\n  try:exe=os.readlink(str(p/'exe'))\n  except OSError:continue\n  if exe=='/usr/local/bin/networksupervisorfixture':os.kill(int(p.name),signal.SIGKILL)")
   for proc in instance.proc.values():assert proc.wait(timeout=3)!=0
   # Hard bounded wait, never a timeout extension. No target SYN has consumed
   # this namespace's one burst, so an unexpired permit would allow this attempt.
   end=time.monotonic()+9
   group='target6' if ':' in target else 'target4'
   while True:
    items=json.loads(h.exec(instance.run,'nft','-j','list','set','inet','dp_run',group))['nftables']
    if not any(x.get('set',{}).get('elem') for x in items):break
    assert time.monotonic()<end;time.sleep(.1)
   answer=instance.actor([dict(ip=target,port=443,tls=True)],'private-manager-killed-expired')
   assert not answer['actions'][0]['connected'] and len(h.events('accept'))==before
   h.record('kernel-expiry-'+str(index),dict(family=4 if index==0 else 6,managers_sigkilled=True,expired_without_cleanup=True,receiver_accepts=0,physical_release=False))
  status=0
 finally:h.record('private-expiry-result',dict(exit_code=status));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
