#!/usr/bin/env python3
"""Reuse the accepted PG18/Owner/Permit/Supervisor harness with a Lite adapter."""
import argparse
import datetime
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import time
ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('approved_harness',ROOT/'tests/targetadmission/https.py')
a=importlib.util.module_from_spec(spec);spec.loader.exec_module(a)
import network_fixture
network_fixture.ALIASES4=[v for v in network_fixture.ALIASES4 if v!='192.0.2.53']+['1.1.1.1']
sys.path.insert(0,str(ROOT/'tests/release'))
import routed_wire as wire
wire.IMAGE='devpilot-private-lite-runtime'

class LiteHarness(a.ApprovedHarness):
 def receiver_source(self):
  return super().receiver_source().replace(b'192.0.2.53',b'1.1.1.1')
 def cli(self,action,active=False):
  command=['python3','-S',str(ROOT/'runtime/private_lite/launcher.py'),'run' if action=='start' else action,'--state',str(self.state)]
  if active:command+=['--config',str(self.config),'--enable-private-lite']
  if action=='start':
   log=open(self.dest/'lite-launcher.stderr','ab')
   proc=subprocess.Popen(command,stdout=log,stderr=log);log.close();self.processes.append(proc)
   end=time.monotonic()+30
   while time.monotonic()<end:
    if proc.poll() is not None:raise RuntimeError('Lite startup failed; inspect private evidence')
    if (self.state/'status.json').exists():
     status=json.loads((self.state/'status.json').read_text())
     if status['pid']==proc.pid and status['event']=='running':return
    time.sleep(.1)
   raise RuntimeError('bounded Lite startup deadline')
  result=subprocess.run(command,capture_output=True,timeout=40)
  (self.dest/('cli-'+action+'.log')).open('ab').write(result.stdout+result.stderr)
  assert result.returncode==0,action
 def review(self,payload):
  request=dict(approval=dict(url=payload['url']),denied_prefixes=['11.0.0.0/8','2600:dead::/32'],resolver='1.1.1.1')
  file=Path(self.private_files.name)/'review-request';file.write_text(json.dumps(request));file.chmod(0o444)
  command=['docker','run','--rm','--network','container:'+self.receiver,'--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','-v',str(self.tools)+':/toolbin:ro','-v',str(file)+':/run/request:ro','--entrypoint','/toolbin/liteapproval','devpilot-scheduler-fixture']
  review=json.loads(self.run(command+['review','--request-file','/run/request']))
  assert review['host']==a.HOST and set(review['addresses'])=={'93.184.216.34','2001:4860:ffff::34'}
  self.record('lite-full-dns-review',review);return review,command,file
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='service-config':
   # Reuse the existing service's five identities/storage binding/configuration.
   result=a.service.ServiceHarness.pg_operation(self,instance,op,payload,selected,mode)
   c=json.loads(self.config.read_text());review,command,file=self.review(payload)
   approval=dict(payload['scope'],url=payload['url'],expires_at=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=10)).isoformat())
   file.chmod(0o600);file.write_text(json.dumps(dict(approval=approval,review=review,resolver='1.1.1.1')));file.chmod(0o444)
   approval=json.loads(self.run(command+['approve','--request-file','/run/request']))
   path=Path(self.private_files.name)/'approval.json';path.write_text(json.dumps(approval));path.chmod(0o644)
   catalog=Path(c['catalog_file']);value=json.loads(catalog.read_text());value.pop('targets');value['approvals']=[approval];catalog.chmod(0o600);catalog.write_text(json.dumps(value));catalog.chmod(0o444)
   image=self.run(['docker','image','inspect',wire.IMAGE,'--format','{{.Id}}']).decode().strip()
   nodes=[]
   for suffix in ('holder','host'):
    cid=self.run(['docker','run','-d','--name',self.prefix+'-lite-'+suffix,'--label',network_fixture.LABEL+'='+self.prefix,'--network','none','--cap-drop','ALL','--cap-add','NET_ADMIN','--security-opt','no-new-privileges:true','--read-only','--memory=128m','--memory-swap=128m','--cpus=1','--pids-limit=128','--ulimit','nofile=256:256','--sysctl','net.ipv4.ip_forward=1','--sysctl','net.ipv6.conf.all.forwarding=1','--sysctl','net.ipv6.conf.all.disable_ipv6=0','--tmpfs','/tmp:rw,noexec,nosuid,size=4m','--tmpfs','/var/lib/postgresql:ro','--entrypoint','sleep',image,'infinity']).decode().strip()
    self.containers.append(cid);nodes.append(cid)
   holder,host=nodes
   wire.wire(holder,'uplink0',host,'lite0');wire.wire(host,'wan0',self.receiver,'edge0')
   for cid,dev,ip in [(holder,'uplink0','198.18.2.2'),(host,'lite0','198.18.2.1'),(host,'wan0','198.18.3.1'),(self.receiver,'edge0','198.18.3.2')]:self.exec(cid,'ip','addr','add',ip+'/30','dev',dev)
   for target in ['1.1.1.1','93.184.216.34']:
    self.exec(holder,'ip','route','add',target+'/32','via','198.18.2.1')
    self.exec(host,'ip','route','add',target+'/32','via','198.18.3.2')
   self.exec(self.receiver,'ip','route','add','198.18.2.0/30','via','198.18.3.1')
   c.update(mode='private-lite',enabled=True,image=image,target_container=holder,target_host=a.HOST,approval_file=str(path),denied_prefixes=['11.0.0.0/8','2600:dead::/32'],lite=dict(resolver='1.1.1.1',host_container=host,host_interface='lite0',source='198.18.2.2'))
   self.config.write_text(json.dumps(c));self.record('lite-topology',dict(no_upstream=True,host=host,holder=holder,resolver='1.1.1.1',old_dns_wire=False));return result
  return super().pg_operation(instance,op,payload,selected,mode)

def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);p.add_argument('--case',choices=['allow','unapproved','dns-change','private4','private6'],default='allow');args=p.parse_args()
 h=LiteHarness(args.evidence.resolve(),args.tools.resolve(),'normal',case=args.case);code=1
 try:h.pg(args.tools/'approved-pg.test','normal');code=0
 finally:h.record('lite-exit',dict(exit_code=code,no_upstream=True,real_host_verified=False));h.close()
 return code
if __name__=='__main__':raise SystemExit(main())
