#!/usr/bin/env python3
"""Only directed v4/v6 kernel expiry and refusal; no SQL, Gate or DB release."""
import argparse,datetime,json,sys,time,uuid
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scheduler'))
from network_fixture import Harness,ALIASES4,ALIASES6,now_string
class ApprovedPolicy(Harness):
 def network_config(self,cfg):
  return dict(cfg,target=self.target,denied=['11.0.0.0/8','2600:dead::/32'],dns_digest='a'*64,approval_expires=now_string(60))
def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 h=ApprovedPolicy(a.evidence,a.tools/'privatenetwork');code=1
 try:
  h.setup_receiver()
  for index,target in enumerate(['93.184.216.34','2001:4860:ffff::34']):
   h.target=target;n=h.instance(index);n.boot(deadline=now_string(25))
   deadline=now_string(8)
   request=dict(**n.identity,host='a.fixture.example.com',scheme='https',ip=target,port=443,deadline=deadline,dns_digest='a'*64)
   install=dict(request=request,permit_id=str(uuid.uuid4()),committed_at=now_string(),permit_deadline=deadline)
   before=len(h.events('accept'));n.install(data=install,ip=target,port=443)
   bad=n.actor([dict(ip=ip,port=443,tls=True) for ip in ALIASES4+ALIASES6],'approved-dual-stack-denials')
   assert not any(x['connected'] for x in bad['actions']) and len(h.events('accept'))==before
   for cid in (n.run,n.gateway):
    h.exec(cid,'python3','-c',"import os,signal,pathlib\nfor p in pathlib.Path('/proc').iterdir():\n if p.name.isdigit():\n  try:exe=os.readlink(str(p/'exe'))\n  except OSError:continue\n  if exe=='/usr/local/bin/networksupervisorfixture':os.kill(int(p.name),signal.SIGKILL)")
   for proc in n.proc.values():assert proc.wait(timeout=3)!=0
   end=time.monotonic()+9;group='target6' if ':' in target else 'target4'
   while True:
    rows=json.loads(h.exec(n.run,'nft','-j','list','set','inet','dp_run',group))['nftables']
    if not any(x.get('set',{}).get('elem') for x in rows):break
    assert time.monotonic()<end;time.sleep(.1)
   answer=n.actor([dict(ip=target,port=443,tls=True)],'approved-expired-no-manager')
   assert not answer['actions'][0]['connected'] and len(h.events('accept'))==before
   h.record('approved-expiry-'+str(index),dict(family=4 if index==0 else 6,synthetic_management_proof=True,managers_sigkilled=True,expired_without_cleanup=True,receiver_accepts=0,physical_release=False))
  code=0
 finally:h.record('result',dict(exit_code=code,no_upstream=True));h.close()
 return code
if __name__=='__main__':raise SystemExit(main())
