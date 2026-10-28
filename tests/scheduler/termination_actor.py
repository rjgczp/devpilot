"""Fixture-only hostile Probe lifecycle witness; not the HTTP execution kernel."""
import json,os,pathlib,signal,socket,ssl,sys,time
from network_actor import run as check_boundary

# The ordinary6.4 actor's exact credential/resource checks are retained.
config=json.loads(sys.stdin.readline())
assert os.getgroups()==[]
boundary=check_boundary({'actions': [],'admin':True,'gateway':config['gateway']})
for path in ('/proc/1/fd/0','/proc/1/fd/1','/proc/1/mem','/proc/1/environ'):
 try:f=open(path,'rb')
 except (PermissionError,FileNotFoundError):continue
 else:f.close();raise AssertionError('private Supervisor state accessible')
try:os.kill(1,signal.SIGTERM)
except PermissionError:pass
else:raise AssertionError('Probe can signal Supervisor')
for sig in (signal.SIGTERM,signal.SIGINT):signal.signal(sig,signal.SIG_IGN)
# An orphan grandchild and a direct child deliberately ignore cooperative stop.
for orphan in (False,True):
 pid=os.fork()
 if pid==0:
  if orphan:
   pid=os.fork()
   if pid!=0:os._exit(0)
  while True:signal.pause()
family=socket.AF_INET6 if ':' in config['ip'] else socket.AF_INET
sock=socket.socket(family,socket.SOCK_STREAM);sock.settimeout(.5)
sock.connect((config['ip'],443))
ctx=ssl.create_default_context(cadata=config['certificate'])
sock=ctx.wrap_socket(sock,server_hostname='a.fixture.example.com')
sock.sendall(b'GET /fixture/stream HTTP/1.1\r\nHost: a.fixture.example.com\r\nConnection: keep-alive\r\n\r\n')
assert sock.recv(512).startswith(b'HTTP/1.1 200')
sock.sendall(b'before-stop\n')
print(json.dumps(dict(event='tls_ready',boundary=boundary,pid=os.getpid(),net_namespace=os.readlink('/proc/self/ns/net'),
 pid_namespace=os.readlink('/proc/self/ns/pid'),monotonic_ns=time.monotonic_ns())),flush=True)
for line in sys.stdin:
 assert json.loads(line)=={'op':'write-after-cut'}
 success=False
 try:sock.sendall(b'after-stop\n');success=True
 except OSError:pass
 print(json.dumps(dict(event='write_attempt',send_returned=success,monotonic_ns=time.monotonic_ns())),flush=True)
while True:signal.pause()
