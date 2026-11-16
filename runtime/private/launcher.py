#!/usr/bin/env python3
"""Small Linux composition root. No listener, migration or public execution.

Only operator-owned config/secret paths are accepted. Existing Docker isolation,
role binaries, typed anonymous IPC and the original PID1 proofs do the work.
"""
import argparse,base64,concurrent.futures,fcntl,json,os,re,select,signal,subprocess,sys,threading,time
from pathlib import Path
from kernel import Kernel,LABEL,SESSION,canonical

HERE=Path(__file__).resolve().parent
UIDS={'supervisor':65531,'controller':65530,'sampler':65529,'watchdog':65529}
def private_dir(p):
 p=Path(p).resolve();p.mkdir(mode=0o700,parents=True,exist_ok=True);s=p.stat();assert s.st_uid==os.getuid() and s.st_mode&0o777==0o700;return p
def ticks(pid):return Path(f'/proc/{pid}/stat').read_text().rsplit(')',1)[1].split()[19]
def load(path):
 p=Path(path).resolve();st=p.stat();assert st.st_uid==os.getuid() and st.st_mode&0o077==0
 c=json.loads(p.read_text());assert c['mode']=='private-isolated' and re.fullmatch('[a-z0-9-]{1,32}',c['runtime_id'])
 assert re.fullmatch('sha256:[0-9a-f]{64}',c['image']) and re.fullmatch('[0-9a-f]{64}',c['target_container']) and re.fullmatch('[0-9a-f]{64}',c['pg_container'])
 assert c['target_ip'] in ('93.184.216.34','2001:4860:ffff::34') and c['target_host']=='a.fixture.example.com','real egress is not enabled by this entry'
 assert len(c['workers'])==2 and c['workers'][0]['incarnation']!=c['workers'][1]['incarnation']
 paths=[x['database_url_file'] for x in c['workers']]+[c[k]['database_url_file'] for k in ('supervisor','controller','reporter')]
 assert len(set(paths))==5
 for value in paths+[c['catalog_file'],c['bindings_file'],c['ca_file']]:
  q=Path(value);assert q.is_absolute() and q.is_file() and not q.is_symlink()
 return c

class Runtime:
 def __init__(self,c,state):
  self.c=c;self.state=state;self.kernel=Kernel(c,state);self.children={};self.pending={};self.lock=threading.RLock();self.closing=False;self.failed=False
  self.guard_counts=[0,0];self.latest={};self.hellos={};self.hello_replies={};self.ready=set();self.done=[];self.closed_channels=set();self.recoveries=[];self.pool=concurrent.futures.ThreadPoolExecutor(max_workers=2)
 def status(self,event,**values):
  value=dict(event=event,pid=os.getpid(),start_ticks=ticks(os.getpid()),runtime_id=self.c['runtime_id'],mode='private-isolated',production=False,**values)
  with self.lock:
   tmp=self.state/'status.tmp';tmp.write_bytes(canonical(value));tmp.chmod(0o600);os.replace(tmp,self.state/'status.json')
 def child(self,name,role,profile,index=None):
  uid=65532+index if role=='worker' else UIDS[role]
  container=self.kernel.prefix+'-'+name
  args=['docker','run','-i','--name',container,'--label',LABEL+'='+self.c['runtime_id'],'--label',SESSION+'='+self.kernel.session,'--network',self.c['control_network'],'--user',f'{uid}:{uid}','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=4k','--memory','256m','--memory-swap','256m','--cpus','1','--pids-limit','128','--ulimit','nofile=256:256','-v',profile['database_url_file']+':/run/own-url:ro']
  if role=='worker':args+=['-v',self.c['catalog_file']+':/run/catalog:ro']
  if role=='supervisor':args+=['-v',str(self.state/'public.pem')+':/run/stop-public.pem:ro']
  if role=='sampler':
   args+=['-v',self.c['bindings_file']+':/run/bindings:ro']
   for src,dst in self.c['metadata_mounts']:args+=['-v',src+':'+dst+':ro']
  args+=['--entrypoint','python3',self.c['image'],'-S','-u','/runtime/control_child.py',role,profile['incarnation']]
  if role=='supervisor':args.append(self.c['workers'][index]['incarnation'])
  err=open(self.state/(name+'.stderr'),'ab',buffering=0);os.chmod(err.name,0o600)
  p=subprocess.Popen(args,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=err,bufsize=0);err.close()
  self.children[name]=p;self.pending[name]=bytearray()
  # Docker starts asynchronously; the child's validated IPC is readiness, not
  # an immediate inspect racing daemon container creation.
  return p
 def send(self,name,channel,raw=None,eof=False):
  p=self.children.get(name)
  if p is None or p.poll() is not None:return
  if (name,channel) in self.closed_channels:return
  if eof:self.closed_channels.add((name,channel))
  value=dict(channel=channel,eof=True) if eof else dict(channel=channel,bytes=base64.b64encode(raw).decode())
  with self.lock:p.stdin.write(canonical(value)+b'\n');p.stdin.flush()
 def stop_child(self,name):
  p=self.children.get(name)
  if p is not None and p.poll() is None:
   with self.lock:
    try:p.stdin.write(b'{"stop":true}\n');p.stdin.flush()
    except BrokenPipeError:pass
 def close_admission(self,failure=False):
  with self.lock:
   self.failed|=failure
   if self.closing:return
   self.stop_at=time.monotonic();self.closing=True;self.status('stopping',failed=self.failed)
  for i in (0,1):self.send('worker'+str(i),'guard',eof=True)
  self.stop_child('sampler')
 def recover(self,index,line,finalize=False):
  if any(x['line']==line for x in self.recoveries):return
  assert len(self.recoveries)<4
  self.recoveries.append(dict(index=index,line=line,finalize=finalize,at=time.monotonic()))
  self.send('controller','recovery',canonical(dict(op='recover',line=line))+b'\n')
 def route(self,name,v):
  if 'exit_code' in v:
   if not self.closing:self.close_admission(True)
   return
  if 'event' in v:
   event=v['event']
   if name.startswith('worker') and event['event']=='worker_ready':
    self.ready.add(name)
    if len(self.ready)==2:self.status('running',workers=2)
   if event['event']=='result_committed':
    run=event['run_id'];assert re.fullmatch('[0-9a-f]{8}-[0-9a-f-]{27}',run)
    owner=json.loads((self.state/(run+'.owner.json')).read_text())
    assert owner['index']==int(name[-1]) and (self.state/(run+'.database-accepted')).exists()
    self.recover(owner['index'],owner['line'],True)
    self.done.append(run);self.status('stopping' if self.closing else 'running',workers=len(self.ready),completed_runs=len(self.done))
   return
  channel=v['channel']
  if v.get('eof'):
   if name.startswith('worker'):self.send('supervisor'+name[-1],'worker',eof=True)
   if name.startswith('supervisor'):
    self.send('worker'+name[-1],'worker',eof=True)
    self.send('controller',name[-1],eof=True)
   if not self.closing:self.close_admission(True)
   return
  raw=base64.b64decode(v['bytes'],validate=True);assert len(raw)<=65536
  if name=='sampler':self.send('watchdog','pulse',raw);return
  if name=='watchdog':
   i=int(channel);assert i in (0,1) and len(raw)==8;self.guard_counts[i]+=1;self.latest[i]=raw
   if not self.closing:self.send('worker'+str(i),'guard',raw)
   return
  if name=='controller':
   if channel=='recovery':
    reply=json.loads(raw);assert self.recoveries
    request=self.recoveries.pop(0)
    assert reply['event']=='recover_ok' and reply['line']==request['line']
    if request['finalize']:self.kernel.call(request['index'],dict(op='recovered',line=request['line']))
   else:self.send('supervisor'+channel,'controller',raw)
   return
  i=int(name[-1]);assert i in (0,1)
  if name.startswith('worker'):
   request=json.loads(raw)
   if request['op']=='hello':
    self.hellos[i]=raw
    if len(self.hellos)==2:
     for n,r in self.hellos.items():self.send('supervisor'+str(n),'worker',r)
   else:self.send('supervisor'+str(i),'worker',raw)
  elif name.startswith('supervisor'):
   if channel=='worker':
    reply=json.loads(raw)
    if reply['event']=='supervisor_ready':
     self.hello_replies[i]=raw
     if len(self.hello_replies)==2:
      for n,r in self.hello_replies.items():self.send('worker'+str(n),'worker',r)
    else:self.send('worker'+str(i),'worker',raw)
   elif channel=='controller':self.send('controller',str(i),raw)
   elif channel=='kernel':
    c=json.loads(raw)
    if self.closing and c['op']=='prepare':raise RuntimeError('new preparation closed')
    future=self.pool.submit(self.kernel.call,i,c);future.add_done_callback(lambda f,i=i:self.kernel_reply(i,f))
   else:raise RuntimeError('unbound control channel')
 def kernel_reply(self,i,future):
  try:self.send('supervisor'+str(i),'kernel',canonical(future.result())+b'\n')
  except BaseException as error:
   print('private kernel rejected: '+type(error).__name__,file=sys.stderr)
   self.send('supervisor'+str(i),'kernel',eof=True);self.close_admission(True)
 def poll(self,seconds=.1):
  if self.recoveries and time.monotonic()-self.recoveries[0]['at']>5:self.close_admission(True)
  live={p.stdout:name for name,p in self.children.items() if p.stdout and not p.stdout.closed}
  for fd in select.select(list(live),[],[],seconds)[0]:
   name=live[fd];raw=os.read(fd.fileno(),65536)
   if not raw:
    fd.close()
    if not self.closing:self.close_admission(True)
    continue
   self.pending[name]+=raw
   while b'\n' in self.pending[name]:
    line,_,rest=self.pending[name].partition(b'\n');self.pending[name]=bytearray(rest);assert len(line)<=131072;self.route(name,json.loads(line))
   assert len(self.pending[name])<=131072
 def run(self):
  network=json.loads(self.kernel.run(['docker','network','inspect',self.c['control_network']]))[0]
  assert network['Internal'] and network['EnableIPv6'] and not network['Ingress']
  pg=self.kernel.inspect(self.c['pg_container'])
  volumes=[m for m in pg['Mounts'] if m['Destination']=='/var/lib/postgresql']
  assert len(volumes)==1 and volumes[0]['Type']=='volume'
  assert pg['HostConfig']['LogConfig']['Type'] in ('json-file','local') and pg['LogPath']
  expected=[[volumes[0]['Name'],'/storage'],[str(Path(pg['LogPath']).parent),'/logs-metadata']]
  assert self.c['metadata_mounts']==expected,'only actual PG/log metadata mounts allowed'
  self.status('starting')
  self.child('controller','controller',self.c['controller'])
  for i in (0,1):self.child('supervisor'+str(i),'supervisor',self.c['supervisor'],i)
  self.child('watchdog','watchdog',self.c['reporter']);self.child('sampler','sampler',self.c['reporter'])
  until=time.monotonic()+15
  while min(self.guard_counts)<2 and not self.closing:
   if time.monotonic()>until:raise RuntimeError('storage startup closed')
   self.poll()
  if not self.closing:
   for i in (0,1):
    self.child('worker'+str(i),'worker',self.c['workers'][i],i);self.send('worker'+str(i),'guard',self.latest[i])
  while not self.closing:self.poll()
  # New admission is closed. Existing physical/Result paths retain their
  # original deadline; no transaction/lease/permit budget is extended here.
  while time.monotonic()-self.stop_at<45 and (self.kernel.active or self.recoveries or any(p.poll() is None for n,p in self.children.items() if n.startswith('worker'))):
   self.poll()
   for i in list(self.kernel.active):
    p=self.children.get('supervisor'+str(i))
    if p is not None and p.poll() is not None:
     line=self.kernel.active[i]['line']
     try:self.kernel.stop(i)
     except BaseException:self.failed=True
     self.recover(i,line)
  return 2 if self.failed else 0
 def close(self):
  self.close_admission(self.failed)
  for i in list(self.kernel.active):
   try:self.kernel.stop(i)
   except BaseException:self.failed=True
  for name in list(self.children):
   if name!='controller':self.stop_child(name)
  until=time.monotonic()+5
  while self.recoveries and time.monotonic()<until:self.poll()
  self.stop_child('controller')
  for p in self.children.values():
   try:p.wait(timeout=5)
   except subprocess.TimeoutExpired:p.terminate()
  self.pool.shutdown(wait=True,cancel_futures=True)
  for name in self.children:
   container=self.kernel.prefix+'-'+name
   try:
    info=self.kernel.inspect(container)
    labels=info['Config']['Labels'] or {}
    if labels.get(LABEL)!=self.c['runtime_id'] or labels.get(SESSION)!=self.kernel.session:continue
    self.kernel.run(['docker','rm','-f',container])
   except RuntimeError:pass
  unverified=[p.name for p in self.state.glob('*.owner.json') if not (self.state/(json.loads(p.read_text())['line']['run_id']+'.receipt')).exists()]
  self.status('recovery_required' if unverified else 'stopped',failed=self.failed,unverified_runs=len(unverified),completed_runs=len(self.done))

def stop(state):
 p=state/'status.json'
 if not p.exists():return 0
 s=json.loads(p.read_text());pid=s['pid']
 try:
  fd=os.pidfd_open(pid,0)
  try:
   assert ticks(pid)==s['start_ticks'];signal.pidfd_send_signal(fd,signal.SIGTERM)
  finally:os.close(fd)
 except ProcessLookupError:return 0
 return 0
def main():
 p=argparse.ArgumentParser();p.add_argument('action',choices=['init','start','run','stop','status']);p.add_argument('--config',type=Path);p.add_argument('--state',type=Path,required=True);p.add_argument('--run-isolated',action='store_true');a=p.parse_args()
 assert not sys.flags.optimize,'validation requires assertions enabled'
 assert sys.platform=='linux','native Linux host required';state=private_dir(a.state)
 if a.action=='init':
  from assets.authority import Authority
  authority=Authority(state);authority.pub.chmod(0o444);print(json.dumps(dict(event='private_state_initialized',public_key_file=str(authority.pub),execution_available=False)));return 0
 if a.action=='status':print((state/'status.json').read_text() if (state/'status.json').exists() else '{"event":"closed"}');return 0
 if a.action=='stop':return stop(state)
 if not a.run_isolated:print('{"event":"private_service_closed","execution_available":false}');return 0
 c=load(a.config)
 if a.action=='start':
  log=open(state/'launcher.stderr','ab',buffering=0);os.chmod(log.name,0o600)
  child=subprocess.Popen([sys.executable,'-S',str(Path(__file__).resolve()),'run','--run-isolated','--config',str(a.config.resolve()),'--state',str(state)],stdin=subprocess.DEVNULL,stdout=log,stderr=log,start_new_session=True,close_fds=True)
  end=time.monotonic()+30
  while time.monotonic()<end:
   if child.poll() is not None:return 2
   if (state/'status.json').exists():
    s=json.loads((state/'status.json').read_text())
    if s['pid']==child.pid and s['event']=='running':print('{"event":"private_service_running","workers":2,"internet":false}');return 0
   time.sleep(.1)
  stop(state);return 2
 lock=open(state/'launcher.lock','a');fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
 for burned in state.iterdir():
  if re.fullmatch('[0-9a-f]{8}-[0-9a-f-]{27}',burned.name):
   assert (state/(burned.name+'.owner.json')).exists(),'incomplete durable Run journal: stay closed'
 for meta in state.glob('*.owner.json'):
  line=json.loads(meta.read_text())['line'];assert (state/(line['run_id']+'.receipt')).exists(),'unverified physical domain: stay closed, no inferred release'
 runtime=Runtime(c,state)
 def closing(*_):runtime.close_admission()
 signal.signal(signal.SIGTERM,closing);signal.signal(signal.SIGINT,closing)
 try:return runtime.run()
 except BaseException as e:
  print("private service stopped: "+type(e).__name__+" "+str(e),file=sys.stderr);runtime.failed=True;return 2
 finally:runtime.close()
if __name__=='__main__':raise SystemExit(main())
