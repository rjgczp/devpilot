"""Fixed no-upstream namespace backend using the accepted PID1 stop protocol.

No database credential, Worker input reconstruction, deployment or public route.
The durable journal burns a Run before allocation. Container removal is cleanup,
never the source of a physical receipt.
"""
import base64,datetime,hashlib,json,os,select,subprocess,time,threading
from pathlib import Path
from assets.authority import Authority,canonical,sync_directory

LABEL='devpilot.private.runtime'
HERE=Path(__file__).resolve().parent
def utc(delay=0):return (datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=delay)).isoformat().replace('+00:00','Z')

class Kernel:
 def __init__(self,config,state):
  # Authority refreshes its public PEM; it was read-only in child mounts.
  public=state/'public.pem'
  if public.exists():public.chmod(0o600)
  self.cfg=config;self.state=state;self.authority=Authority(state);self.authority.pub.chmod(0o444);self.active={};self.count=0;self.sign_lock=threading.Lock();self.locks=[threading.RLock(),threading.RLock()]
  self.target=config['target_container'];self.image=config['image'];self.prefix='dp-private-'+config['runtime_id']
  self.validate_target()
 def run(self,args,data=None):
  p=subprocess.run(args,input=data,capture_output=True,timeout=20)
  if p.returncode:raise RuntimeError('kernel command rejected')
  return p.stdout
 def inspect(self,cid):return json.loads(self.run(['docker','inspect',cid]))[0]
 def exec(self,cid,*args,data=None):return self.run(['docker','exec','-i',cid,*args],data)
 def validate_target(self):
  t=self.inspect(self.target);h=t['HostConfig']
  assert h['NetworkMode']=='none' and not h['PortBindings'] and not h['Privileged']
  links=json.loads(self.exec(self.target,'ip','-j','link','show'));assert all(x['ifname']=='lo' for x in links),'target has an unbound upstream/old domain'
  for family in ('-4','-6'):assert not json.loads(self.exec(self.target,'ip',family,'-j','route','show','default'))
 def create(self,name,pid=None):
  args=['docker','run','-d','--name',name,'--label',LABEL+'='+self.cfg['runtime_id'],'--network','none','--cap-drop','ALL','--cap-add','NET_ADMIN','--security-opt','no-new-privileges:true','--read-only','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=4k','--memory','128m','--cpus','1','--pids-limit','128','--ulimit','nofile=256:256','--tmpfs','/tmp:rw,noexec,nosuid,size=4m','--sysctl','net.ipv6.conf.all.disable_ipv6=0','--sysctl','net.ipv6.conf.all.accept_ra=0','--sysctl','net.ipv6.conf.default.accept_ra=0','--sysctl','net.ipv4.ip_forward='+('1' if name.endswith('-guard') else '0'),'--sysctl','net.ipv6.conf.all.forwarding='+('1' if name.endswith('-guard') else '0')]
  if pid:args+=['--pid','container:'+pid]
  return self.run(args+['--entrypoint','sleep',self.image,'infinity']).decode().strip()
 def read(self,proc,seconds=3):
  end=time.monotonic()+seconds;raw=bytearray()
  while len(raw)<=65536:
   if not select.select([proc.stdout],[],[],max(0,end-time.monotonic()))[0]:raise RuntimeError('kernel confirmation timeout')
   b=os.read(proc.stdout.fileno(),1)
   if not b:raise RuntimeError('kernel management EOF')
   raw+=b
   if b==b'\n':return json.loads(raw)
  raise RuntimeError('kernel frame bound')
 def send(self,p,value):p.stdin.write(canonical(value)+b'\n');p.stdin.flush()
 def prepare(self,index,line):
  assert index not in self.active
  self.authority.register(line) # O_EXCL+fsync before any allocation/restart.
  owner=self.state/(line['run_id']+'.owner.json')
  with owner.open('xb') as f:f.write(canonical(dict(index=index,line=line)));f.flush();os.fsync(f.fileno())
  sync_directory(self.state)
  self.count+=1;generation=self.count;name=self.prefix+'-'+line['run_id'];run=self.create(name);guard=self.create(name+'-guard',run)
  slot=index+1;probe4=f'198.19.64.{slot*16+2}';gate4=f'198.19.64.{slot*16+3}'
  probe6=f'fd64:64:{generation:x}::2';gate6=f'fd64:64:{generation:x}::3';back4=f'198.19.128.{slot*4+1}';rx4=f'198.19.128.{slot*4+2}';back6=f'fd64:65:{generation:x}::1';rx6=f'fd64:65:{generation:x}::2';rx='dp'+str(generation)
  d=dict(line=line,name=name,run=run,guard=guard,configs={},managers={},life=None,rx=rx);self.active[index]=d
  self.exec(guard,'ip','link','add','eth0','type','veth','peer','name','runpeer');self.exec(guard,'ip','link','set','runpeer','netns','1');self.exec(run,'ip','link','set','runpeer','name','eth0')
  self.run(['docker','run','--rm','--network','container:'+guard,'--pid','container:'+self.target,'--cap-drop','ALL','--cap-add','NET_ADMIN','--security-opt','no-new-privileges:true','--entrypoint','sh',self.image,'-c',f'ip link add eth1 type veth peer name {rx} && ip link set {rx} netns 1'])
  for cid,dev,a,b,mask in [(run,'eth0',probe4,probe6,28),(guard,'eth0',gate4,gate6,28),(guard,'eth1',back4,back6,30),(self.target,rx,rx4,rx6,30)]:
   self.exec(cid,'ip','addr','add',a+'/'+str(mask),'dev',dev);self.exec(cid,'ip','link','set',dev,'addrgenmode','none');self.exec(cid,'ip','-6','addr','add',b+'/64','dev',dev,'nodad');self.exec(cid,'ip','link','set',dev,'up')
  mac=lambda cid,dev:json.loads(self.exec(cid,'ip','-j','link','show',dev))[0]['address']
  rm,gm,bm,xm=mac(run,'eth0'),mac(guard,'eth0'),mac(guard,'eth1'),mac(self.target,rx)
  for cid,dev,a,b,m in [(run,'eth0',gate4,gate6,gm),(guard,'eth0',probe4,probe6,rm),(guard,'eth1',rx4,rx6,xm),(self.target,rx,back4,back6,bm)]:
   for family,ip in [('-4',a),('-6',b)]:self.exec(cid,'ip',family,'neigh','replace',ip,'lladdr',m,'nud','permanent','dev',dev)
  for ip,family,mask in [('93.184.216.34','-4','/32'),('192.0.2.53','-4','/32'),('2001:4860:ffff::34','-6','/128'),('fd64:53::53','-6','/128')]:
   self.exec(run,'ip',family,'route','replace',ip+mask,'via',gate4 if family=='-4' else gate6,'dev','eth0');self.exec(guard,'ip',family,'route','replace',ip+mask,'via',rx4 if family=='-4' else rx6,'dev','eth1')
  for ip,family,via,mask in [(probe4,'-4',back4,'/32'),(probe6,'-6',back6,'/128')]:self.exec(self.target,'ip',family,'route','replace',ip+mask,'via',via,'dev',rx)
  ready=[]
  for role,cid in [('gateway',guard),('run',run)]:
   fp=json.loads(self.exec(cid,'/toolbin/privatenetwork','fingerprint'));cfg=dict(lineage=line,role=role,**fp,probe4=probe4,probe6=probe6,host=self.cfg['target_host'],target=self.cfg['target_ip'],denied=['11.0.0.0/8','2600:dead::/32'],hard_deadline=utc(34));d['configs'][role]=cfg
   p=subprocess.Popen(['docker','exec','-i',cid,'/toolbin/privatenetwork'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,bufsize=0);d['managers'][role]=p;self.send(p,dict(op='boot',config=cfg));ready.append(self.read(p))
  return dict(event='prepared',line=line,ready=ready)
 def launch(self,d,c):
  core=(HERE/'assets/termination_core.py').read_text();life=(HERE/'assets/lifecycle.py').read_text();bootstrap=(HERE/'assets/probe_bootstrap.py').read_text()
  # Same accepted private lifetime pipe adaptation, never a second stop engine.
  life=life.replace("cfg=message();assert cfg['op']=='boot'","life_r,life_w=os.pipe();os.dup2(life_r,19,inheritable=True);os.close(life_r)\ncfg=message();assert cfg['op']=='boot'").replace("elif op=='inspect':","elif op=='lose-lifetime':os.close(life_w);emit(dict(event='lifetime_lost'))\n  elif op=='inspect':").replace('close_fds=True,preexec_fn=drop','close_fds=True,pass_fds=(19,),preexec_fn=drop')
  # Wait only within the original35s hard deadline for the existing10s
  # HTTP timeout. A lost bridge must not turn a pending observation into a
  # dead management channel before PID1 can produce its original stop proof.
  life=life.replace("select.select([child.stdout],[],[],3)","select.select([child.stdout],[],[],min(12,max(0,hard-time.monotonic())))")
  bootstrap=bootstrap.replace('original=[]',"life=fcntl.fcntl(19,fcntl.F_DUPFD_CLOEXEC,30);os.close(19)\noriginal=[]").replace('child=subprocess.Popen(',"os.dup2(life,7,inheritable=True);os.close(life)\nchild=subprocess.Popen(").replace('pass_fds=(3,4,5,6)','pass_fds=(3,4,5,6,7)').replace('for fd in [3,4,5,6,er,ar,qw,rr]','for fd in [3,4,5,6,7,er,ar,qw,rr]').replace('/usr/local/bin/schedulerprobefixture','/toolbin/privateprobe')
  init=core+'\nBOOTSTRAP_SOURCE='+repr(bootstrap)+'\n'+life
  args=['docker','run','--rm','-i','--name',d['name']+'-pid1','--label',LABEL+'='+self.cfg['runtime_id'],'--network','container:'+d['run'],'--cap-drop','ALL']
  for cap in ['KILL','SETUID','SETGID','SETPCAP']:args+=['--cap-add',cap]
  args+=['--security-opt','no-new-privileges:true','--read-only','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=4k','--memory','128m','--memory-swap','128m','--cpus','1','--pids-limit','128','--ulimit','nofile=256:256','--tmpfs','/tmp:rw,noexec,nosuid,size=4m','-v',self.cfg['ca_file']+':/etc/ssl/certs/private-target.pem:ro','--entrypoint','python3',self.image,'-S','-u','-c',init]
  p=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,bufsize=0);d['life']=p
  deadline=int(datetime.datetime.fromisoformat(d['configs']['run']['hard_deadline'].replace('Z','+00:00')).timestamp()*1e9)
  self.send(p,dict(op='boot',lineage=d['line'],deadline_ns=deadline));assert self.read(p)['event']=='lifecycle_ready'
  self.send(p,dict(op='launch',lineage=d['line'],envelope=c['envelope'],receipt=c['receipt']));return self.read(p)
 def call(self,index,c):
  assert index in (0,1)
  with self.locks[index]:return self._call(index,c)
 def _call(self,index,c):
  op=c['op']
  if op=='pending':
   for p in sorted(self.state.glob('*.owner.json')):
    owner=json.loads(p.read_text());line=owner['line'];run=line['run_id']
    if owner['index']==index and (self.state/(run+'.receipt')).exists() and not (self.state/(run+'.database-accepted')).exists():
     return dict(event='pending',line=line,receipt=base64.b64encode((self.state/(run+'.receipt')).read_bytes()).decode(),bundle=base64.b64encode((self.state/(run+'.proofs')).read_bytes()).decode())
   return dict(event='none',line={})
  line=c['line']
  if op=='accepted':
   assert (self.state/(line['run_id']+'.receipt')).exists()
   p=self.state/(line['run_id']+'.database-accepted')
   if not p.exists():
    with p.open('xb') as f:f.write(b'physical receipt accepted; not an HTTP result\n');f.flush();os.fsync(f.fileno())
    sync_directory(self.state)
   return dict(event='accepted',line=line)
  if op=='prepare':return self.prepare(index,line)
  d=self.active[index];assert d['line']==line
  if op=='launch':r=self.launch(d,c)
  elif op=='install':
   assert c['request']['lineage']==line
   install=dict(request=c['request'],permit_id=c['permit']['permit_id'],committed_at=c['permit']['committed_at'],permit_deadline=c['permit']['permit_deadline'])
   for role in ['gateway','run']:
    p=d['managers'][role];self.send(p,dict(op='install',install=install));r=self.read(p);assert r['permit_id']==install['permit_id']
   r=dict(event='installed',confirmation=r)
  elif op in ('next','reply'):
   req=dict(op='next') if op=='next' else dict(op='permit-reply',frame=c['frame']) if c.get('frame') else dict(op='permit-denied')
   self.send(d['life'],dict(lineage=line,**req));r=self.read(d['life'],12 if op=='next' else 3)
  elif op=='stop':return self.stop(index)
  else:raise RuntimeError('unsupported kernel operation')
  r['line']=line
  if 'result' in r:r['observation']=r.pop('result')
  return r
 def stop(self,index):
  with self.locks[index]:return self._stop(index)
 def _stop(self,index):
  d=self.active[index];line=d['line'];network=[]
  for role,cid in [('gateway',d['guard']),('run',d['run'])]:
   network.append(json.loads(self.exec(cid,'/toolbin/privatenetwork',data=canonical(dict(op='revoke',config=d['configs'][role]))+b'\n')))
  p=d['life'];assert p is not None
  self.send(p,dict(op='stop',lineage=line));process=self.read(p);assert process['event']=='processes_terminated'
  self.send(p,dict(op='retire',lineage=line));retired=self.read(p);p.stdin.close();assert p.wait(timeout=3)==0
  with self.sign_lock:receipt=self.authority.sign(line,network,process,retired)
  bundle=(self.state/(line['run_id']+'.proofs')).read_bytes()
  for cid in [d['guard'],d['run']]:
   info=self.inspect(cid);assert info['Config']['Labels'][LABEL]==self.cfg['runtime_id'];self.run(['docker','rm','-f',cid])
  del self.active[index]
  return dict(event='stopped',line=line,receipt=base64.b64encode(canonical(receipt)).decode(),bundle=base64.b64encode(bundle).decode())
