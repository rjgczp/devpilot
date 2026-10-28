#!/usr/bin/env python3
"""Original Go kernel + real PG18 + independent no-upstream Supervisor.

The sole private driver transport carries the sealed frame in memory. Its
persisted log records only digests, never the envelope/configuration. No target
route exists beyond the separately owned receiver namespace.
"""
import argparse,base64,datetime,hashlib,json,os,re,subprocess,time
from pathlib import Path
from network_fixture import HERE,IMAGE,LABEL,now_string
from termination_fixture import StopHarness,Lifecycle,canonical

def permit_command(payload):
 # The Go fixture serializes a denied nil []byte as an explicit JSON null.
 # That is EOF denial, never a success frame or a missing required field.
 if not isinstance(payload,dict) or set(payload)!={'frame'}:raise ValueError('invalid_permit_reply_fields')
 frame=payload['frame']
 if frame is None:return dict(op='permit-denied')
 if not isinstance(frame,str):raise ValueError('invalid_permit_reply_type')
 try:raw=base64.b64decode(frame,validate=True)
 except (ValueError,TypeError):raise ValueError('invalid_permit_reply_encoding') from None
 if len(raw)!=2048:raise ValueError('invalid_permit_reply_size')
 return dict(op='permit-reply',frame=frame)

def redact_joint_line(line):
 if not line.startswith(b'NETWORK64 '):return line
 x=json.loads(line[len(b'NETWORK64 '):])
 if x['op']=='reply':
  command=permit_command(x['payload'])
  # This exact null-only IPC has no URL, credentials or capability bytes. Keep
  # its original bytes as evidence rather than inventing an empty allow frame.
  if command['op']=='permit-denied':return line
 elif x['op'] in ['launch','replay']:
  if not isinstance(x['payload'],dict) or set(x['payload'])!={'envelope','receipt'}:raise ValueError('invalid_handoff_fields')
 else:return line
 sizes={'envelope':4096,'receipt':1024,'frame':2048}
 redacted={}
 for k,v in x['payload'].items():
  if not isinstance(v,str):raise ValueError('invalid_capability_type')
  raw=base64.b64decode(v,validate=True)
  if len(raw)!=sizes[k]:raise ValueError('invalid_capability_size')
  redacted[k]=dict(length=len(raw),sha256=hashlib.sha256(raw).hexdigest())
 x['payload']=redacted
 return b'NETWORK64-REDACTED '+json.dumps(x).encode()+b'\n'

class JointLifecycle(Lifecycle):
 def __init__(self,h,instance):
  self.h,self.instance=h,instance;self.identity=instance.identity.copy();self.stopped=False
  h.authority.register(self.identity)
  # Exactly the existing physical stop helper definitions, not a substitute
  # kill/reap implementation. The new source only adapts launch/pipe plumbing.
  definitions=(HERE/'termination_init.py').read_text().split('assert os.getpid()==1')[0]
  init=definitions+'\nBOOTSTRAP_SOURCE='+repr((HERE/'joint_bootstrap.py').read_text())+'\n'+(HERE/'joint_init.py').read_text()
  cert=h.dest/'public-fixture-ca.crt';cert.write_text(h.certificate)
  cmd=['docker','run','--rm','-i','--name',h.prefix+'-'+instance.name+'-joint','--label',LABEL+'='+h.prefix,
       '--network','container:'+instance.run,'--cap-drop','ALL','--cap-add','KILL','--cap-add','SETUID','--cap-add','SETGID','--cap-add','SETPCAP',
       '--security-opt','no-new-privileges:true','--read-only','--memory=128m','--memory-swap=128m','--cpus=1','--pids-limit=128','--ulimit','nofile=256:256',
       '--tmpfs','/tmp:rw,noexec,nosuid,size=4m','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=4k',
       '-v',str(h.probe)+':/usr/local/bin/schedulerprobefixture:ro','-v',str(cert)+':/etc/ssl/certs/devpilot-fixture.pem:ro',
       '--entrypoint','python3',IMAGE,'-u','-c',init]
  h.record(instance.name+'-lifecycle-command',dict(command=cmd[:-1]+['<pinned-joint-init-source>'],source_sha256=hashlib.sha256(init.encode()).hexdigest(),probe_sha256=hashlib.sha256(h.probe.read_bytes()).hexdigest()))
  self.proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(h.dest/(instance.name+'-lifecycle.stderr'),'wb'));h.processes.append(self.proc);h.lifecycle.append(self)
  deadline=int(datetime.datetime.fromisoformat(instance.config['run']['hard_deadline'].replace('Z','+00:00')).timestamp()*1e9)
  self.send(dict(op='boot',lineage=self.identity,deadline_ns=deadline));self.ready=h.readline(self.proc);assert self.ready['event']=='lifecycle_ready'
  self.name=h.prefix+'-'+instance.name+'-joint';self.container=json.loads(h.run(['docker','inspect',self.name]))[0]
  h.record(instance.name+'-lifecycle-container',self.container)
  host=self.container['HostConfig'];assert host['PidMode']=='' and not host['Privileged'] and host['Memory']==134217728 and host['MemorySwap']==134217728 and host['NanoCpus']==1000000000 and host['PidsLimit']==128
  assert len(self.container['Mounts'])==2 and all(m['Destination'] in ['/usr/local/bin/schedulerprobefixture','/etc/ssl/certs/devpilot-fixture.pem'] and m['Type']=='bind' and not m['RW'] for m in self.container['Mounts'])
  assert host['Tmpfs']=={'/tmp':'rw,noexec,nosuid,size=4m','/var/lib/postgresql':'ro,noexec,nosuid,size=4k'}
  assert self.ready['net_namespace']==instance.ready['run']['namespace']
 def send(self,value):
  raw=json.dumps(value).encode()+b'\n';assert len(raw)<=8192;self.proc.stdin.write(raw);self.proc.stdin.flush()
 def private_launch(self,payload):
  self.send(dict(op='launch',lineage=self.identity,**payload));answer=self.h.readline(self.proc)
  # The trusted pipe frames are never recorded. Result/proof projections only.
  self.h.record('probe-input-digests',{k:hashlib.sha256(base64.b64decode(payload[k])).hexdigest() for k in ['envelope','receipt']})
  self.h.record('probe-sealed',answer);return answer

 def call(self,op,**values):
  answer=super().call(op,**values)
  if op in ('inspect','fault-tracer'):
   self.inspect_count=getattr(self,'inspect_count',0)+1
   self.h.record(self.instance.name+'-proc-snapshot-'+str(self.inspect_count),answer)
   root=next(x for x in answer['pids'] if x['pid']==1)
   assert root['status']['TracerPid']=='0','Supervisor is controlled by a tracer'
  if op=='stop':
   raw=bytearray()
   while True:
    self.send(dict(op='supervision-events',lineage=self.identity,offset=len(raw)))
    part=self.h.readline(self.proc);assert part['event']=='supervision_events' and part['offset']==len(raw)
    raw+=base64.b64decode(part['chunk'],validate=True)
    if len(raw)==part['total']:break
    assert len(raw)<part['total']<=65536
   assert hashlib.sha256(raw).hexdigest()==part['sha256']
   audit=json.loads(raw);assert audit['lineage']==self.identity
   proof_pids={x['pid'] for x in answer['pids']}
   opened={x['pid'] for x in audit['events'] if x['event']=='pidfd_open'}
   killed={x['pid'] for x in audit['events'] if x['event']=='signal_before' and x['signal']==9}
   waited={x['pid'] for x in audit['events'] if x['event']=='waitpid'}
   assert proof_pids and proof_pids<=opened and proof_pids<=killed and proof_pids<=waited
   self.h.record(self.instance.name+'-supervision-events',audit)
  return answer

class JointHarness(StopHarness):
 def __init__(self,dest,binary,probe,case,mode):
  super().__init__(dest,binary);self.probe=probe.resolve();self.case=case;self.mode=mode;self.life=None;self.request_seen=False
 def pg_test_name(self):return '^TestBudgetJointKernelPG$'
 def public_catalog(self,selected):return dict(ip=selected,stop_public_key=self.authority.pub.read_text(),case=self.case)
 def receiver_source(self):
  return ('JOINT_MODE='+repr(self.case)+'\nJOINT_FAMILY='+str(4 if self.mode=='normal' else 6)+'\n').encode()+(HERE/'joint_receiver.py').read_bytes()
 def driver_log_line(self,line):
  return redact_joint_line(line)
 def install_fast(self,instance,payload):
  # Both live rule verifications happen inside the independently running Go
  # managers. Slow evidence snapshots follow ACK, never extend pipe timeout.
  assert {x['qtype'] for x in self.events('dns')}=={1,28},'permit preceded full DNS validation'
  self.record(instance.name+'-install-request',payload)
  if self.case=='supervisor-lost':
   for proc in instance.proc.values():proc.stdin.close();assert proc.wait(timeout=3)==0
   self.record('install-denied',dict(reason='independent management channel lost'));return dict(confirmation=None)
  if self.case=='rules-missing':self.exec(instance.run,'nft','delete','table','inet','dp_run')
  confirmations=[]
  for role in ['gateway','run']:
   proc=instance.proc[role]
   proc.stdin.write(json.dumps(dict(op='install',install=payload)).encode()+b'\n');proc.stdin.flush()
   try:answer=self.readline(proc)
   except RuntimeError:
    assert self.case=='rules-missing';self.record('install-denied',dict(reason='rule verification failed',role=role));return dict(confirmation=None)
   assert answer['permit_id']==payload['permit_id'];confirmations.append(answer)
  self.record('both-target-rules-confirmed',dict(confirmations=confirmations,at=now_string()))
  return dict(confirmation=confirmations[-1])
 def pg_operation(self,instance,op,payload,selected,mode):
  if op=='launch':
   self.life=JointLifecycle(self,instance)
   sealed=self.life.private_launch(payload)
   if sealed['event']=='sealed':
    proof=sealed['proof'];assert proof['tcp_only'] and not proof['sockets_denied'] and proof['fd_limit']==256 and proof['no_new_privileges'] and proof['capabilities_zero'] and proof['secrets_unavailable']
   return sealed
  if op=='next':
   self.life.send(dict(op='next',lineage=self.life.identity));answer=self.readline(self.life.proc)
   if answer['event']=='target_request':
    assert not self.request_seen;self.request_seen=True
    self.record('kernel-request-after-dns',dict(dns=self.events('dns'),at=now_string()))
   else:self.record('probe-observation',answer)
   return answer
  if op=='install-joint':return self.install_fast(instance,payload)
  if op=='reply':
   command=permit_command(payload)
   self.life.send(dict(lineage=self.life.identity,**command));answer=self.readline(self.life.proc)
   assert answer['event']==('reply_denied' if command['op']=='permit-denied' else 'reply_delivered');return answer
  if op=='replay':
   self.life.send(dict(op='launch',lineage=self.life.identity,**payload));answer=self.readline(self.life.proc);assert answer['event']=='replay_rejected';self.record('same-run-replay-refused',answer);return answer
  if op=='held-tls':
   until=time.monotonic()+2
   while not self.events('held_tls'):
    assert time.monotonic()<until;time.sleep(.01)
   assert len(self.events('get'))==1 and len(self.events('accept'))==2
   instance.snapshot('tls-before-independent-stop');return dict(event='held_tls')
  if op=='finish-no-input':
   assert not self.events('accept') and self.life is None
   instance.snapshot('no-input-closed');return dict(event='no_input_zero_target')
  if op=='finish':
   if self.case=='cross-run-envelope':
    # Input was rejected before a receiving capability existed. Do not turn a
    # guessed fast process exit into a signed proof or a returned physical slot.
    assert not self.events('accept');instance.snapshot('rejected-input');return dict(event='input_rejected_quarantine')
   count=len(self.events('get'))
   inflight=self.case in ['tls-stop','tracer-stopped','tracer-crashed','supervisor-killed','stop-ack-lost','journal-collision']
   is_success=self.case in ['allow-http','allow-https','duplicate-envelope'] or inflight
   assert count==(1 if is_success else 0),(self.case,self.events())
   accepts=[x for x in self.events('accept') if x['port'] in (80,443)]
   assert len(accepts)==(1 if is_success or self.case=='tls-invalid' else 0),(self.case,accepts)
   if is_success:
    get=self.events('get')[0];assert get['is_get'] and get['header_only'] and not get['credentials']
    if self.case!='allow-http':assert get['sni']=='a.fixture.example.com'
    if not inflight:assert len(self.events('header_only_close'))==1
   # Every trace authority is below PID1, never attached to PID1. Include the
   # tracer and relay in the real pidfd/reap domain, not only the Go PID.
   live=self.life.call('inspect');desc=[x for x in live['pids'] if x['pid']!=1]
   assert len(desc)>=3 and all(x['status']['Uid'].split()[0]=='65534' and int(x['status']['CapEff'],16)==0 for x in desc)
   tracer=next(x for x in desc if x['status']['Name']=='strace')
   assert tracer['status']['PPid']=='1'
   assert any(x['status']['TracerPid']==str(tracer['pid']) for x in desc)
   if self.case in ['tracer-stopped','tracer-crashed']:
    fault=self.life.call('fault-tracer',action='stop' if self.case=='tracer-stopped' else 'kill')
    self.record('tracer-fault',fault)
   if self.case=='supervisor-killed':
    self.run(['docker','kill','--signal','KILL',self.life.name]);assert self.life.proc.wait(timeout=3)!=0
   network=self.life.revoke(require_live=inflight)
   if self.case=='supervisor-killed':
    assert not (self.authority.path/(self.life.identity['run_id']+'.receipt')).exists()
    self.record('unconfirmed-stop',dict(reason=self.case,receipt=False,capacity_released=False))
    return dict(event='unconfirmed_stop_quarantine')
   if self.case=='stop-ack-lost':
    self.life.send(dict(op='stop',lineage=self.life.identity))
    # Real stop is requested; deliberately lose its ACK over the real pipe.
    # Let the existing3s acknowledgement budget expire, never sign/read back a
    # guessed outcome or reuse the Probe capability.
    time.sleep(3.05)
    assert not (self.authority.path/(self.life.identity['run_id']+'.receipt')).exists()
    self.record('unconfirmed-stop',dict(reason=self.case,receipt=False,capacity_released=False,actual_wait_seconds=3.05))
    return dict(event='unconfirmed_stop_quarantine')
   if self.case=='tracer-crashed':
    self.life.call('stop')
    # Complete retirement/audit evidence was lost. A process-exit observation
    # alone cannot create an attestation, even after this conservative kill.
    assert not (self.authority.path/(self.life.identity['run_id']+'.receipt')).exists()
    self.record('unconfirmed-stop',dict(reason=self.case,receipt=False,capacity_released=False))
    return dict(event='unconfirmed_stop_quarantine')
   if self.case=='journal-collision':
    path=self.authority.path/(self.life.identity['run_id']+'.proofs')
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
    with os.fdopen(fd,'wb') as stream:stream.write(b'injected journal collision, not a physical proof');stream.flush();os.fsync(stream.fileno())
    try:self.life.stop(network,consume=False)
    except FileExistsError:pass
    else:raise AssertionError('journal fault produced receipt')
    assert not (self.authority.path/(self.life.identity['run_id']+'.receipt')).exists()
    self.record('unconfirmed-stop',dict(reason=self.case,receipt=False,capacity_released=False))
    return dict(event='unconfirmed_stop_quarantine')
   receipt=self.life.stop(network,consume=False)
   bundle=json.loads((self.authority.path/(self.life.identity['run_id']+'.proofs')).read_bytes());self.record('committed-stop-bundle',bundle)
   trace=(self.dest/(instance.name+'-probe.trace')).read_text()
   # DUMPABLE=0 can make sockaddr memory opaque to ptrace. Count every actual
   # connect syscall across every child, and account for the independently
   # witnessed single TCP DNS session. No other connect may be hidden/ignored.
   connect_calls=re.findall(r'^\d+\s+connect\(',trace,re.MULTILINE)
   dns_accepts=[x for x in self.events('accept') if x['port']==53]
   assert len(dns_accepts)==1 and len(connect_calls)==1+len(accepts),(self.case,len(connect_calls),len(accepts))
   assert 'private-path-SENTINEL' not in trace and 'DB_PASSWORD_SENTINEL' not in trace
   self.record('joint-kernel-witness',dict(case=self.case,total_connect_syscalls=len(connect_calls),dns_connects=1,target_connect_syscalls=len(connect_calls)-1,target_accepts=len(accepts),get_count=count,physical_stop=True,at=now_string()))
   return dict(event='physically_stopped',stop_receipt=receipt,stop_bundle=bundle)
  raise RuntimeError('unknown joint operation')
 def pg_completion(self,mode,status):return dict(case=self.case,mode=mode,exit_code=status,original_kernel=True,production_execution=False)

def main():
 p=argparse.ArgumentParser();p.add_argument('--evidence',type=Path,required=True);p.add_argument('--supervisor',type=Path,required=True);p.add_argument('--probe',type=Path,required=True);p.add_argument('--pg-binary',type=Path,required=True);p.add_argument('--mode',choices=['normal','race'],required=True);p.add_argument('--case',required=True);a=p.parse_args()
 h=JointHarness(a.evidence.resolve(),a.supervisor,a.probe,a.case,a.mode);status=1
 try:h.pg(a.pg_binary.resolve(),a.mode);status=0
 finally:h.record('joint63-result',dict(exit_code=status,case=a.case,mode=a.mode));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
