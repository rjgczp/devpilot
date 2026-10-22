"""Trusted Linux PID1 lifecycle supervisor. Private IPC, no network/DB secrets.

Only KILL/SETUID/SETGID/SETPCAP are held. The launched hostile fixture Probe
execs with all capability sets empty. PIDfds retain identity across PID reuse;
PID1 adopts orphan descendants. A bounded scan/stop/kill/reap must reach zero
processes before an empty-domain proof. No Worker callback participates.
"""
import base64,ctypes,hashlib,json,os,pathlib,resource,select,signal,subprocess,sys,time


def message():
 line=sys.stdin.buffer.readline(4097)
 if not line:return None
 if len(line)>4096 or not line.endswith(b'\n'):raise RuntimeError('frame')
 return json.loads(line)


def emit(value):print(json.dumps(value),flush=True)


def status():
 return dict((k,v.strip()) for line in pathlib.Path('/proc/self/status').read_text().splitlines() if ':' in line for k,v in [line.split(':',1)])


def drop():
 libc=ctypes.CDLL(None,use_errno=True)
 # Drop the entire bounding set before SETPCAP is lost; exec clears the rest.
 for cap in range(41):
  if libc.prctl(24,cap,0,0,0)!=0:os._exit(111)
 os.setgroups([]);os.setgid(65534);os.setuid(65534)
 if libc.prctl(38,1,0,0,0)!=0 or libc.prctl(4,0,0,0,0)!=0:os._exit(112)
 resource.setrlimit(resource.RLIMIT_CORE,(0,0));resource.setrlimit(resource.RLIMIT_FSIZE,(65536,65536));os.umask(0o077)


def pids():return sorted(int(x.name) for x in pathlib.Path('/proc').iterdir() if x.name.isdecimal() and int(x.name)!=1)


def reap():
 while True:
  try:
   pid,_=os.waitpid(-1,os.WNOHANG)
   if pid==0:return
  except ChildProcessError:return


def stop():
 global stopped,proof
 stopped=True
 end=time.monotonic()+2;observed={};started=time.monotonic_ns()
 try:
  while True:
   reap();live=pids()
   if not live:break
   if len(live)>127 or time.monotonic()>=end:raise RuntimeError('unconfirmed')
   for pid in live:
    if pid not in observed:
     try:
      fd=os.pidfd_open(pid,0)
      stat=pathlib.Path(f'/proc/{pid}/stat').read_text();ticks=stat[stat.rfind(')')+2:].split()[19]
      observed[pid]=(fd,ticks)
     except ProcessLookupError:continue
    fd,_=observed[pid]
    try:signal.pidfd_send_signal(fd,signal.SIGSTOP)
    except ProcessLookupError:pass
   for fd,_ in observed.values():
    try:signal.pidfd_send_signal(fd,signal.SIGKILL)
    except ProcessLookupError:pass
   time.sleep(.005)
  exits=[]
  for pid,(fd,ticks) in observed.items():
   if not select.select([fd],[],[],0)[0]:raise RuntimeError('pidfd_not_exited')
   exits.append(dict(pid=pid,start_ticks=ticks,pidfd_exited=True))
  # The domain is closed, so there is no remaining process that could fork.
  assert not pids()
  proof=dict(event='processes_terminated',lineage=identity,net_namespace=os.readlink('/proc/self/ns/net'),
             pid_namespace=os.readlink('/proc/self/ns/pid'),pids=exits,remaining_pids=[],
             launch_consumed=True,at_ns=time.time_ns(),started_monotonic_ns=started,finished_monotonic_ns=time.monotonic_ns())
  return proof
 finally:
  for fd,_ in observed.values():os.close(fd)


assert os.getpid()==1 and os.getuid()==0
s=status();assert all(s[k]=='00000000000001e0' for k in ('CapEff','CapPrm','CapBnd'))
assert s['NoNewPrivs']=='1'
libc=ctypes.CDLL(None,use_errno=True)
assert libc.prctl(4,0,0,0,0)==0
assert libc.prctl(36,1,0,0,0)==0 # subreaper as well as PID1
cfg=message();assert cfg['op']=='boot'
identity=cfg['lineage'];deadline=cfg['deadline_ns']
remaining=(deadline-time.time_ns())/1e9;assert 0<remaining<=35
hard=time.monotonic()+remaining
stopped=False;proof=None;child=None
emit(dict(event='lifecycle_ready',lineage=identity,net_namespace=os.readlink('/proc/self/ns/net'),pid_namespace=os.readlink('/proc/self/ns/pid')))
try:
 while True:
  if time.monotonic()>=hard:
   emit(stop());break
  if not select.select([sys.stdin],[],[],max(0,hard-time.monotonic()))[0]:continue
  req=message()
  if req is None:
   emit(stop());break
  if req.get('lineage')!=identity:raise RuntimeError('binding')
  op=req['op']
  if op=='launch' and child is None and not stopped:
   # Both sources are fixed, hashed fixture code passed by the trusted allocator.
   source=ACTOR_SOURCE
   child=subprocess.Popen(['/usr/bin/strace','-f','-e','trace=network,process','-o','/tmp/probe.trace','/usr/bin/python3','-u','-c',source],stdin=subprocess.PIPE,stdout=subprocess.PIPE,
                          stderr=sys.stderr,env={'LANG':'C','LC_ALL':'C','TZ':'UTC'},close_fds=True,preexec_fn=drop)
   child.stdin.write(json.dumps(req['config']).encode()+b'\n');child.stdin.flush()
   if not select.select([child.stdout],[],[],2)[0]:raise RuntimeError('child_ready')
   ready=json.loads(child.stdout.readline(4097));assert ready['event']=='tls_ready'
   emit(ready)
  elif op=='write-after-cut' and child is not None and not stopped:
   child.stdin.write(b'{"op":"write-after-cut"}\n');child.stdin.flush()
   if not select.select([child.stdout],[],[],2)[0]:raise RuntimeError('child_write')
   emit(json.loads(child.stdout.readline(4097)))
  elif op=='inspect':
   rows=[]
   for pid in pids():
    text=pathlib.Path(f'/proc/{pid}/status').read_text()
    fields=dict((k,v.strip()) for line in text.splitlines() if ':' in line for k,v in [line.split(':',1)])
    rows.append(dict(pid=pid,status={k:fields[k] for k in ('Name','State','PPid','Uid','Gid','CapEff','CapPrm','CapBnd','NoNewPrivs')},stat=pathlib.Path(f'/proc/{pid}/stat').read_text()))
   emit(dict(event='process_snapshot',pids=rows,at_ns=time.time_ns()))
  elif op=='stop':emit(stop())
  elif op=='trace' and stopped and proof and not pids():
   # Read the immutable dead-child trace with only the already-held SETUID
   # capability; no DAC override, Docker mount or extra process is needed.
   libc.setfsuid(65534)
   try:data=pathlib.Path('/tmp/probe.trace').read_bytes()
   finally:libc.setfsuid(0)
   assert len(data)<=65536
   offset=req['offset'];assert isinstance(offset,int) and 0<=offset<=len(data)
   emit(dict(event='trace',offset=offset,total=len(data),sha256=hashlib.sha256(data).hexdigest(),chunk=base64.b64encode(data[offset:offset+2048]).decode()))
  elif op=='retire' and stopped and proof and not pids():
   emit(dict(event='pid_namespace_retiring',pid_namespace=proof['pid_namespace'],lineage=identity));break
  else:raise RuntimeError('consumed')
except BaseException:
 # Control loss never leaves a permissive child lifecycle. Missing confirmation
 # still means quarantine to the external verifier, even if this kill succeeds.
 try:stop()
 except BaseException:pass
 raise
