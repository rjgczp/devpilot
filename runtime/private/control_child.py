"""Anonymous pipe adapter inside one cap0, role-specific control container.

No role receives another role's secret mount. Outer routing carries only the
existing bounded control frames; Probe is never in this management domain.
"""
import base64,fcntl,json,os,select,subprocess,sys
role,inc,*extra=sys.argv[1:]
uids={'worker':(65532,65533),'supervisor':(65531,),'controller':(65530,),'sampler':(65529,),'watchdog':(65529,)}
assert role in uids and os.getuid() in uids[role]
pairs={'worker':3,'supervisor':6,'controller':6,'sampler':1,'watchdog':3}[role]
raw=[]
for _ in range(pairs):raw+=list(os.pipe())
fds=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,30) for fd in raw]
for fd in raw:os.close(fd)
reads={};writers={};passed=[];kept=[]
def request_pair(index,readfd,writefd,label):
 r,w,qr,qw=fds[index:index+4];os.dup2(r,readfd,inheritable=True);os.dup2(qw,writefd,inheritable=True)
 writers[label]=w;reads[qr]=label;passed.extend([readfd,writefd]);kept.extend([w,qr])
if role=='worker':
 request_pair(0,3,4,'worker');gr,gw=fds[4:6];os.dup2(gr,5,inheritable=True);writers['guard']=gw;kept.append(gw);passed.append(5)
 cmd=['/toolbin/privateworker','--database-url-file','/run/own-url','--incarnation',inc,'--private-isolated','--storage-watchdog','--catalog-file','/run/catalog']
elif role=='supervisor':
 # fd3/4: Worker server; fd5/6: kernel client; fd7/8: Controller client.
 request_pair(0,3,4,'worker')
 for offset,a,b,label in [(4,5,6,'kernel'),(8,7,8,'controller')]:
  r,w,qr,qw=fds[offset:offset+4];os.dup2(r,a,inheritable=True);os.dup2(qw,b,inheritable=True)
  writers[label]=w;reads[qr]=label;kept.extend([w,qr]);passed.extend([a,b])
 cmd=['/toolbin/privatesupervisor','--run','--role','supervisor','--database-url-file','/run/own-url','--incarnation',inc,'--worker-incarnation',extra[0],'--stop-public-key-file','/run/stop-public.pem']
elif role=='controller':
 for offset,a,b,label in [(0,3,4,'0'),(4,5,6,'1'),(8,7,8,'recovery')]:request_pair(offset,a,b,label)
 cmd=['/toolbin/privatesupervisor','--run','--role','controller','--database-url-file','/run/own-url','--incarnation',inc]
elif role=='sampler':
 r,w=fds;os.dup2(w,3,inheritable=True);reads[r]='pulse';kept.append(r);passed=[3]
 cmd=['/toolbin/storagereporter','--run','--database-url-file','/run/own-url','--incarnation',inc,'--bindings-file','/run/bindings']
else:
 r,w,a,aw,b,bw=fds;os.dup2(r,3,inheritable=True);os.dup2(aw,4,inheritable=True);os.dup2(bw,5,inheritable=True)
 writers['pulse']=w;reads[a]='0';reads[b]='1';kept.extend([w,a,b]);passed=[3,4,5]
 cmd=['/toolbin/storagereporter','--watchdog','--database-url-file','/run/own-url','--incarnation',inc]
child=subprocess.Popen(cmd,stdin=subprocess.DEVNULL,stdout=subprocess.PIPE,stderr=sys.stderr,pass_fds=tuple(passed),close_fds=True,bufsize=0,env={'LANG':'C','TZ':'UTC'})
for fd in set(fds+passed):
 if fd not in kept:os.close(fd)
pending={fd:bytearray() for fd in reads};stdin=bytearray();stdout=bytearray()
def emit(value):print(json.dumps(value,separators=(',',':')),flush=True)
try:
 while child.poll() is None:
  ready=select.select([0,child.stdout,*reads],[],[],.2)[0]
  if 0 in ready:
   part=os.read(0,65536)
   if not part:break
   stdin+=part
   while b'\n' in stdin:
    line,_,rest=stdin.partition(b'\n');stdin=bytearray(rest);assert len(line)<=131072;v=json.loads(line)
    if v=={'stop':True}:child.terminate();continue
    label=v['channel'];assert label in writers
    if v.get('eof'):
     os.close(writers.pop(label));continue
    data=base64.b64decode(v['bytes'],validate=True);assert len(data)<=65536
    if label in ('guard','pulse'):assert len(data)==8
    os.write(writers[label],data)
   assert len(stdin)<=131072
  for fd,label in list(reads.items()):
   if fd not in ready:continue
   data=os.read(fd,65536)
   if not data:
    emit(dict(channel=label,eof=True));os.close(fd);del reads[fd];continue
   if role in ('sampler','watchdog'):
    pending[fd]+=data
    while len(pending[fd])>=8:
     beat=pending[fd][:8];pending[fd]=pending[fd][8:];emit(dict(channel=label,bytes=base64.b64encode(beat).decode()))
   else:
    pending[fd]+=data;assert len(pending[fd])<=131072
    while b'\n' in pending[fd]:
     line,_,rest=pending[fd].partition(b'\n');pending[fd]=bytearray(rest);assert len(line)<65536
     emit(dict(channel=label,bytes=base64.b64encode(line+b'\n').decode()))
  if child.stdout in ready:
   data=os.read(child.stdout.fileno(),8192)
   if not data:break
   stdout+=data;assert len(stdout)<=16384
   while b'\n' in stdout:
    line,_,rest=stdout.partition(b'\n');stdout=bytearray(rest);event=json.loads(line)
    assert set(event)<=set(['event','gate','execution_available','run_id','physical_release'])
    emit(dict(event=event))
finally:
 for fd in writers.values():
  try:os.close(fd)
  except OSError:pass
 if child.poll() is None:child.terminate()
 try:child.wait(timeout=5)
 except subprocess.TimeoutExpired:child.kill();child.wait()
 emit(dict(exit_code=child.returncode))
