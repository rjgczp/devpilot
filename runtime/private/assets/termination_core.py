"""Trusted Linux PID1 lifecycle supervisor. Private IPC, no network/DB secrets.

Only KILL/SETUID/SETGID/SETPCAP are held. The launched hostile fixture Probe
execs with all capability sets empty. PIDfds retain identity across PID reuse;
PID1 adopts orphan descendants. A bounded scan/stop/kill/reap must reach zero
processes before an empty-domain proof. No Worker callback participates.
"""

def require(condition, message='private boundary rejected'):
 if not condition:raise RuntimeError(message)

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
  require(not pids())
  proof=dict(event='processes_terminated',lineage=identity,net_namespace=os.readlink('/proc/self/ns/net'),
             pid_namespace=os.readlink('/proc/self/ns/pid'),pids=exits,remaining_pids=[],
             launch_consumed=True,at_ns=time.time_ns(),started_monotonic_ns=started,finished_monotonic_ns=time.monotonic_ns())
  return proof
 finally:
  for fd,_ in observed.values():os.close(fd)


