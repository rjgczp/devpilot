"""6.3 pipe adapter to the unchanged6.5 PID1 stop()/pidfd/reap helpers.

PID1 remains untraced. Only its capability-free child domain is traced. The
outer allocator embeds the unchanged helper definitions and fixed relay source.
"""

# Observation wrappers delegate every original syscall and preserve its return
# or exception. They do not change stop decisions, pidfd checks or proof fields.
supervision_events=[];fd_pids={}
def journal(kind,**values):
 if len(supervision_events)>=512:raise RuntimeError('bounded_supervision_journal')
 supervision_events.append(dict(event=kind,at_ns=time.time_ns(),**values))
original_pidfd_open=os.pidfd_open
original_pidfd_signal=signal.pidfd_send_signal
original_waitpid=os.waitpid
def observed_open(pid,flags=0):
 fd=original_pidfd_open(pid,flags);fd_pids[fd]=pid;journal('pidfd_open',pid=pid,fd=fd);return fd
def observed_signal(fd,sig,info=None,flags=0):
 journal('signal_before',fd=fd,pid=fd_pids.get(fd),signal=int(sig))
 result=original_pidfd_signal(fd,sig,info,flags);journal('signal_after',fd=fd,pid=fd_pids.get(fd),signal=int(sig));return result
def observed_wait(pid,flags):
 result=original_waitpid(pid,flags)
 if result[0]:journal('waitpid',pid=result[0],status=result[1],flags=flags)
 return result
os.pidfd_open=observed_open;signal.pidfd_send_signal=observed_signal;os.waitpid=observed_wait

def message():
 line=sys.stdin.buffer.readline(8193)
 if not line:return None
 if len(line)>8192 or not line.endswith(b'\n'):raise RuntimeError('frame')
 return json.loads(line)
def proc_snapshot():
 rows=[]
 for pid in [1]+pids():
  try:
   fields=dict((k,v.strip()) for line in pathlib.Path(f'/proc/{pid}/status').read_text().splitlines() if ':' in line for k,v in [line.split(':',1)])
   rows.append(dict(pid=pid,status={k:fields[k] for k in ('Name','State','PPid','TracerPid','Uid','Gid','CapEff','CapPrm','CapBnd','NoNewPrivs')},wchan=pathlib.Path(f'/proc/{pid}/wchan').read_text(),stat=pathlib.Path(f'/proc/{pid}/stat').read_text()))
  except FileNotFoundError:pass
 return dict(event='process_snapshot',pids=rows,at_ns=time.time_ns())
def independent():
 assert status()['TracerPid']=='0','stop authority must never be traced'
def relay(req):
 child.stdin.write(json.dumps(req).encode()+b'\n');child.stdin.flush()
 if not select.select([child.stdout],[],[],3)[0]:raise RuntimeError('bounded_relay_event')
 line=child.stdout.readline(8193)
 if not line or len(line)>8192:raise RuntimeError('relay_closed')
 return json.loads(line)

assert os.getpid()==1 and os.getuid()==0
s=status();assert all(s[k]=='00000000000001e0' for k in ('CapEff','CapPrm','CapBnd')) and s['NoNewPrivs']=='1'
independent();libc=ctypes.CDLL(None,use_errno=True);assert libc.prctl(4,0,0,0,0)==0 and libc.prctl(36,1,0,0,0)==0
cfg=message();assert cfg['op']=='boot';identity=cfg['lineage'];deadline=cfg['deadline_ns']
remaining=(deadline-time.time_ns())/1e9;assert 0<remaining<=35
hard=time.monotonic()+remaining;stopped=False;proof=None;child=None
emit(dict(event='lifecycle_ready',lineage=identity,net_namespace=os.readlink('/proc/self/ns/net'),pid_namespace=os.readlink('/proc/self/ns/pid')))
try:
 while True:
  if time.monotonic()>=hard and not stopped:independent();emit(stop());break
  if not select.select([sys.stdin],[],[],3 if stopped else max(0,hard-time.monotonic()))[0]:
   if stopped:break
   continue
  req=message()
  if req is None:independent();emit(stop());break
  if req.get('lineage')!=identity:raise RuntimeError('binding')
  independent();op=req['op']
  if op=='launch' and child is None and not stopped:
   # Restores the accepted6.5 topology. No daemonized tracer around PID1.
   # The relay uses only the standard library. Automatic site initialization
   # expands the absent HOME through pwd.getpwuid/glibc NSS before our code,
   # causing two unneeded NSCD socket attempts. Do not initialize user sites.
   child=subprocess.Popen(['/usr/bin/strace','-f','-e','trace=network,process,signal','-o','/tmp/probe.trace','/usr/bin/python3','-S','-u','-c',BOOTSTRAP_SOURCE],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=sys.stderr,env={'LANG':'C','LC_ALL':'C','TZ':'UTC'},bufsize=0,close_fds=True,preexec_fn=drop)
   emit(relay(dict(envelope=req['envelope'],receipt=req['receipt'])))
  elif op=='launch':emit(dict(event='replay_rejected'))
  elif op in ('next','permit-reply','permit-denied') and child is not None and not stopped:
   emit(relay({k:v for k,v in req.items() if k not in ('lineage',)}))
  elif op=='inspect':emit(proc_snapshot())
  elif op=='fault-tracer' and child is not None and not stopped:
   action=req['action'];assert action in ('stop','kill')
   fd=os.pidfd_open(child.pid,0)
   try:
    signal.pidfd_send_signal(fd,signal.SIGSTOP if action=='stop' else signal.SIGKILL)
    if action=='kill':assert select.select([fd],[],[],1)[0],'crashed tracer pidfd must actually exit'
    else:
     end=time.monotonic()+1
     while not pathlib.Path(f'/proc/{child.pid}/status').read_text().split('State:',1)[1].strip().startswith('T'):
      assert time.monotonic()<end;time.sleep(.005)
   finally:os.close(fd)
   emit(proc_snapshot())
  elif op=='stop':
   before=proc_snapshot();journal('stop_snapshot',pids=[dict(pid=x['pid'],state=x['status']['State'],tracer=x['status']['TracerPid']) for x in before['pids']]);emit(stop())
  elif op=='supervision-events' and stopped and proof and not pids():
   raw=json.dumps(dict(lineage=identity,events=supervision_events),separators=(',',':')).encode();assert len(raw)<=65536
   offset=req['offset'];assert isinstance(offset,int) and 0<=offset<=len(raw)
   emit(dict(event='supervision_events',offset=offset,total=len(raw),sha256=hashlib.sha256(raw).hexdigest(),chunk=base64.b64encode(raw[offset:offset+2048]).decode()))
  elif op=='trace' and stopped and proof and not pids():
   libc.setfsuid(65534)
   try:data=pathlib.Path('/tmp/probe.trace').read_bytes()
   finally:libc.setfsuid(0)
   assert len(data)<=65536;offset=req['offset'];assert isinstance(offset,int) and 0<=offset<=len(data)
   emit(dict(event='trace',offset=offset,total=len(data),sha256=hashlib.sha256(data).hexdigest(),chunk=base64.b64encode(data[offset:offset+2048]).decode()))
  elif op=='retire' and stopped and proof and not pids():emit(dict(event='pid_namespace_retiring',pid_namespace=proof['pid_namespace'],lineage=identity));break
  else:raise RuntimeError('consumed')
except BaseException:
 try:independent();stop()
 except BaseException:pass
 raise
