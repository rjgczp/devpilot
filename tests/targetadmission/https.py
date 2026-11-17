#!/usr/bin/env python3
import argparse,datetime,hashlib,importlib.util,json,os,subprocess,sys,time
from pathlib import Path
HERE=Path(__file__).resolve().parent;ROOT=HERE.parents[1]
spec=importlib.util.spec_from_file_location('accepted_service_harness',HERE.parent/'privateservice/lifecycle.py')
service=importlib.util.module_from_spec(spec);spec.loader.exec_module(service)
ServiceHarness=service.ServiceHarness
HOST='monitor.owned-test.example.com';EDGE='edge.owned-test.example.com'
class ApprovedHarness(ServiceHarness):
 def __init__(self,*args,case="allow"):
  super().__init__(*args);self.case_under_test=case;self.pg_test_pattern='TestPrivateApprovedPG' if case=='allow' else 'TestPrivateApprovedRefusalPG';self.expected_gets=3 if case=='allow' else 0;self.task_name='approved-https';self.pg_deadline_seconds=480
 def cli(self,action,active=False):
  cmd=['sh',str(ROOT/'scripts/private-service.sh'),action,'--state',str(self.state)]
  if active:cmd+=['--config',str(self.config),'--run-approved']
  p=subprocess.run(cmd,capture_output=True,timeout=40)
  (self.dest/('cli-'+action+'.log')).open('ab').write(p.stdout+p.stderr)
  assert p.returncode==0,'approved service '+action+' failed; preserved logs'
 def receiver_source(self):
  data=super().receiver_source().decode().replace('a.fixture.example.com',HOST)
  start=data.index("                if MODE=='rebinding'")
  end=data.index("                answer=query[:2]",start)
  dns='''                labels=[];pos=12
                while query[pos]:
                    size=query[pos];labels.append(query[pos+1:pos+1+size].decode());pos+=size+1
                name='.'.join(labels)
                def wire(n):return b''.join(bytes([len(x)])+x.encode() for x in n.split('.'))+b'\\0'
                if name==HOST:
                    packed=wire(EDGE);rr=b'\\xc0\\x0c'+struct.pack('!HHIH',5,1,30,len(packed))+packed;count=1
                else:
                    assert name==EDGE
                    address=('10.0.0.9' if os.path.exists('/tmp/private4') else '93.184.216.35' if os.path.exists('/tmp/dns-change') else '93.184.216.34') if typ==1 else ('fd00::9' if os.path.exists('/tmp/private6') else '2001:4860:ffff::34')
                    packed=socket.inet_pton(socket.AF_INET if typ==1 else socket.AF_INET6,address)
                    rr=b'\\xc0\\x0c'+struct.pack('!HHIH',typ,1,30,len(packed))+packed;count=1
'''.replace('HOST',repr(HOST)).replace('EDGE',repr(EDGE))
  return (data[:start]+dns+data[end:]).encode()
 def review(self,payload):
  # The same cap0 DNS-only tool runs in the owned receiver namespace with no
  # default/upstream route. Root never forwards a user-supplied DNS response.
  request=json.dumps(dict(approval=dict(url=payload['url']),denied_prefixes=['11.0.0.0/8','2600:dead::/32'])).encode()
  self.exec(self.receiver,'sh','-c','cat > /tmp/review-request.json',data=request)
  base=['docker','run','--rm','--network','container:'+self.receiver,'--user','65534:65534','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','-v',str(self.tools)+':/toolbin:ro','--entrypoint','/toolbin/privateapproval','devpilot-scheduler-fixture']
  # Request comes through a read-only file, not the Probe or management socket.
  file=Path(self.private_files.name)/'review-request';file.write_bytes(request);file.chmod(0o444)
  # Insert the sole read-only request before the image.
  image=base.index('devpilot-scheduler-fixture');base[image:image]=['-v',str(file)+':/run/request:ro']
  review=json.loads(self.run(base+['review','--request-file','/run/request']))
  assert review['host']==HOST and set(review['addresses'])=={'93.184.216.34','2001:4860:ffff::34'}
  self.record('owned-dns-review',review)
  return review,base,file
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='service-config':
   result=super().pg_operation(instance,op,payload,selected,mode)
   c=json.loads(self.config.read_text());review,base,file=self.review(payload)
   a=dict(payload['scope'],url=payload['url'],expires_at=(datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(minutes=10)).isoformat())
   file.chmod(0o600);file.write_text(json.dumps(dict(approval=a,review=review)));file.chmod(0o444)
   a=json.loads(self.run(base+['approve','--request-file','/run/request']))
   approval=Path(self.private_files.name)/'operator-approval.json';approval.write_text(json.dumps(a));approval.chmod(0o644)
   catalog=Path(c['catalog_file']);cat=json.loads(catalog.read_text());cat.pop('targets');cat['approvals']=[a];catalog.chmod(0o600);catalog.write_text(json.dumps(cat));catalog.chmod(0o444)
   c.update(mode='private-approved',target_host=HOST,approval_file=str(approval),denied_prefixes=['11.0.0.0/8','2600:dead::/32']);self.config.write_text(json.dumps(c))
   self.record('approved-binding',dict(scope=payload['scope'],dns_digest=a['dns_digest'],expires_at=a['expires_at'],internet=False))
   return result
  if op=='service-refusal-start':
   if self.case_under_test=='unapproved':
    c=json.loads(self.config.read_text());p=Path(c['catalog_file']);v=json.loads(p.read_text());v['approvals'][0]['monitor_id']='01900000-0000-7000-8000-000000000099';p.chmod(0o600);p.write_text(json.dumps(v));p.chmod(0o444);self.cli('start',True)
   elif self.case_under_test=='approval-expired':
    c=json.loads(self.config.read_text());p=Path(c['approval_file']);v=json.loads(p.read_text());v['expires_at']=(datetime.datetime.now(datetime.timezone.utc)-datetime.timedelta(seconds=1)).isoformat();p.write_text(json.dumps(v))
    cmd=['sh',str(ROOT/'scripts/private-service.sh'),'start','--config',str(self.config),'--state',str(self.state),'--run-approved'];r=subprocess.run(cmd,capture_output=True,timeout=15)
    assert r.returncode!=0
    (self.dest/'expected-expired-rejection.log').write_bytes(r.stdout+r.stderr)
   else:
    assert self.case_under_test in ('dns-change','private4','private6')
    self.exec(self.receiver,'touch','/tmp/'+self.case_under_test);self.cli('start',True)
   return dict(frame=__import__('base64').b64encode(self.case_under_test.encode()).decode())
  if op=='service-refusal-assert':
   assert not self.events('get'),'refused target sent GET'
   self.record('refusal-result',dict(case=self.case_under_test,target_gets=0,exit_code=0,internet=False));return dict(event='refusal_verified')
  if op=='service-assert':
   result=super().pg_operation(instance,op,payload,selected,mode)
   assert all(g['sni']==HOST for g in self.events('get'))
   self.record('approved-https-result',dict(real_tls=True,owner_authorized=True,mode='private-approved',gets=3,workers=2,completed=2,unknown=1,restart_no_resend=True,supervisor_loss_closed=True,history=True,upstream=False))
   return result
  return super().pg_operation(instance,op,payload,selected,mode)

def main():
 p=argparse.ArgumentParser();p.add_argument('--case',choices=['allow','unapproved','approval-expired','dns-change','private4','private6'],default='allow');p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 assert sys.platform=='linux','native Linux required, never use host route'
 h=ApprovedHarness(a.evidence.resolve(),a.tools.resolve(),'normal',case=a.case);code=1
 try:h.pg(a.tools/'approved-pg.test','normal');code=0
 finally:h.record('exit',dict(exit_code=code,internet=False));h.close()
 return code
if __name__=='__main__':raise SystemExit(main())
