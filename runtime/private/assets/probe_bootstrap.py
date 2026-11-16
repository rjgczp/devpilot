"""Credential-free child relay traced like the accepted6.5 Probe.

Only stdio exists when strace forks. The single-use anonymous descriptors are
allocated here afterwards, so the tracer cannot keep their writers alive.
Neither this relay nor Go receives the Supervisor management pipe or secrets.
"""
import base64,fcntl,json,os,select,subprocess,sys,time

assert sys.flags.no_site==1 and 'site' not in sys.modules,'relay must not initialize user sites/NSS'

def read_message():
 line=sys.stdin.buffer.readline(8193)
 if not line or len(line)>8192 or not line.endswith(b'\n'):raise RuntimeError('bounded_relay_input')
 return json.loads(line)
def emit(value):print(json.dumps(value),flush=True)
def exact(fd,size):
 result=bytearray();end=time.monotonic()+2
 while len(result)<size:
  if not select.select([fd],[],[],max(0,end-time.monotonic()))[0]:raise RuntimeError('bounded_pipe')
  part=os.read(fd,size-len(result))
  if not part:raise RuntimeError('truncated_pipe')
  result+=part
 return bytes(result)

def deliver_permit(req,fd):
 if fd is None:raise RuntimeError('permit_reply_consumed')
 try:
  if req=={'op':'permit-denied'}:
   # Zero bytes and EOF are the original Go Client's denial protocol. Never
   # synthesize a Confirmation, an empty success frame or a second writer.
   return 'reply_denied'
  if set(req)!={'op','frame'} or req['op']!='permit-reply' or not isinstance(req['frame'],str):raise RuntimeError('invalid_permit_reply')
  frame=base64.b64decode(req['frame'],validate=True)
  if len(frame)!=2048:raise RuntimeError('invalid_permit_reply_size')
  if os.write(fd,frame)!=len(frame):raise RuntimeError('partial_permit_reply')
  return 'reply_delivered'
 finally:os.close(fd)

assert os.getuid()==65534 and os.getgid()==65534 and os.getgroups()==[]
fields=dict((k,v.strip()) for l in open('/proc/self/status') if ':' in l for k,v in [l.split(':',1)])
assert all(fields[k]=='0000000000000000' for k in ('CapEff','CapPrm','CapBnd','CapInh','CapAmb')) and fields['NoNewPrivs']=='1'
assert dict(os.environ)=={'LANG':'C','LC_ALL':'C','TZ':'UTC'}
config=read_message();envelope=base64.b64decode(config['envelope'],validate=True);ack=base64.b64decode(config['receipt'],validate=True)
assert len(envelope)==4096 and len(ack)==1024
original=[]
for _ in range(4):original+=list(os.pipe())
copies=[fcntl.fcntl(fd,fcntl.F_DUPFD_CLOEXEC,20) for fd in original]
for fd in original:os.close(fd)
er,ew,ar,aw,qr,qw,rr,rw=copies
for target,fd in enumerate([er,ar,qw,rr],3):os.dup2(fd,target,inheritable=True)
child=subprocess.Popen(['/usr/local/bin/schedulerprobefixture'],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=sys.stderr,env={'LANG':'C','TZ':'UTC'},bufsize=0,close_fds=True,pass_fds=(3,4,5,6))
for fd in [3,4,5,6,er,ar,qw,rr]:os.close(fd)
os.write(ew,envelope);os.close(ew);os.write(aw,ack);os.close(aw)
if not select.select([child.stdout],[],[],2)[0]:raise RuntimeError('bounded_probe_seal')
line=child.stdout.readline(4097)
if not line:emit(dict(event='input_rejected'));sys.exit(0)
sealed=json.loads(line);assert sealed['event']=='sealed';emit(sealed)
while True:
 req=read_message()
 if req['op']=='next':
  available=select.select([child.stdout]+([qr] if qr is not None else []),[],[],2.5)[0]
  if qr is not None and qr in available:
   frame=exact(qr,2048)
   # EOF remains mandatory, and only Go now holds the writer.
   if not select.select([qr],[],[],2)[0] or os.read(qr,1)!=b'':raise RuntimeError('pipe_not_closed')
   os.close(qr);qr=None;emit(dict(event='target_request',frame=base64.b64encode(frame).decode()))
  elif child.stdout in available:
   observation=json.loads(child.stdout.readline(4097));assert observation['event']=='observation';emit(observation)
  else:raise RuntimeError('bounded_probe_event')
 elif req['op'] in ('permit-reply','permit-denied'):
  event=deliver_permit(req,rw);rw=None;emit(dict(event=event))
 else:raise RuntimeError('relay_consumed')
