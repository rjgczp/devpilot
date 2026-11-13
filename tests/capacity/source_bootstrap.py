"""Non-root control-only relay; native sampler/watchdog remain separate processes."""
import base64,fcntl,json,os,select,subprocess,sys
assert os.getuid()==65534
kind,inc=sys.argv[1:];count=1 if kind=='sampler' else 3
original=[]
for _ in range(count):original+=list(os.pipe())
fds=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,30) for fd in original]
for fd in original:os.close(fd)
if kind=='sampler':
 read,write=fds;os.dup2(write,3,inheritable=True);reads={read:None};keep=[read]
 args=['--run','--bindings-file','/run/bindings'];passed=(3,)
else:
 read,write,a,aw,b,bw=fds;os.dup2(read,3,inheritable=True);os.dup2(aw,4,inheritable=True);os.dup2(bw,5,inheritable=True)
 reads={a:0,b:1};keep=[write,a,b];args=['--watchdog'];passed=(3,4,5)
cmd=['/toolbin/storagereporter','--database-url-file','/run/source-url','--incarnation',inc]+args
child=subprocess.Popen(cmd,stdin=subprocess.DEVNULL,stdout=sys.stderr,stderr=sys.stderr,pass_fds=passed,close_fds=True,env={'LANG':'C','TZ':'UTC'})
for fd in set(fds+list(passed)):
 if fd not in keep:os.close(fd)
while child.poll() is None:
 ready=select.select([sys.stdin,*reads],[],[],.2)[0]
 if sys.stdin in ready:
  raw=sys.stdin.buffer.readline(8193)
  if not raw:child.terminate();break
  value=json.loads(raw)
  if value.get('op')=='stop':child.terminate();break
  if kind=='watchdog':
   if value.get('source_closed'):os.close(write);break
   pulse=base64.b64decode(value['pulse']);assert len(pulse)==8;os.write(write,pulse)
 for fd,index in reads.items():
  if fd not in ready:continue
  pulse=os.read(fd,8)
  if not pulse:print(json.dumps(dict(lost=True)),flush=True);child.wait(timeout=5);raise SystemExit(0)
  assert len(pulse)==8
  out=dict(pulse=base64.b64encode(pulse).decode())
  if index is not None:out['index']=index
  print(json.dumps(out),flush=True)
try:child.wait(timeout=5)
except subprocess.TimeoutExpired:child.kill();child.wait()
