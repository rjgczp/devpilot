"""Caps0 control-container relay. No target wire/admin/DB administrator secret.

Only this native Worker gets its restricted DSN and catalog files. The relay
owns anonymous IPC; requests go to the independently trusted outer Supervisor.
"""
import fcntl,json,os,select,subprocess,sys
assert os.getuid()==65534
original=[]
for _ in range(2):original+=list(os.pipe())
rr,rw,qr,qw=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,20) for fd in original]
for fd in original:os.close(fd)
os.dup2(rr,3,inheritable=True);os.dup2(qw,4,inheritable=True)
child=subprocess.Popen(['/toolbin/privateworker','--database-url-file','/run/worker-url','--incarnation',sys.argv[1],'--private-isolated','--catalog-file','/run/worker-catalog'],stdout=subprocess.PIPE,stderr=sys.stderr,close_fds=True,pass_fds=(3,4),env={'LANG':'C','TZ':'UTC'},bufsize=0)
for fd in set([rr,qw,3,4]):
 if fd not in [rw,qr]:os.close(fd)
request=bytearray();event=bytearray()
while True:
 ready=select.select([sys.stdin,qr,child.stdout],[],[],1)[0]
 if sys.stdin in ready:
  line=sys.stdin.buffer.readline(65537)
  if not line:break
  assert len(line)<=65536 and line.endswith(b'\n')
  value=json.loads(line)
  if value.get('op')=='stop':child.terminate();break
  assert set(value)=={'reply'}
  raw=json.dumps(value['reply']).encode()+b'\n';assert len(raw)<=65536;os.write(rw,raw)
 if qr in ready:
  data=os.read(qr,1)
  if data:request+=data
  if request.endswith(b'\n'):
   assert len(request)<=65536
   print(json.dumps(dict(kind='request',request=json.loads(request))),flush=True);request.clear()
 if child.stdout in ready:
  data=os.read(child.stdout.fileno(),1)
  if not data:
   status=child.wait(timeout=3);print(json.dumps(dict(kind='exit',exit_code=status)),flush=True);break
  event+=data;assert len(event)<=8192
  if event.endswith(b'\n'):print(json.dumps(dict(kind='event',event=json.loads(event))),flush=True);event.clear()
 if child.poll() is not None and not ready:break
try:child.wait(timeout=5)
except subprocess.TimeoutExpired:child.kill();child.wait()
