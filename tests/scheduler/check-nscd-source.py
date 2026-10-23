#!/usr/bin/env python3
"""Real Linux NSS attribution controls; no target network or audit exceptions.

The positive controls intentionally invoke passwd lookup in a network-none,
caps0 fixture. They must produce exactly the two observed NSCD attempts. The
actual relay startup/imports with -S must have no socket/connect calls at all.
No MonkeyPatch/LD_PRELOAD is applied to the acceptance Probe or Supervisor.
"""
import argparse,ast,json,os,re,subprocess,sys
from pathlib import Path


def inside(relay):
 assert os.getuid()==65534
 status=Path('/proc/self/status').read_text()
 fields=dict(line.split(':',1) for line in status.splitlines() if ':' in line)
 assert all(int(fields[k].strip(),16)==0 for k in ['CapEff','CapPrm','CapBnd','CapInh','CapAmb'])
 assert fields['NoNewPrivs'].strip()=='1'
 # Import exactly the current relay's standard library modules, not a hand
 # copied list that could omit a future dependency introducing a socket.
 imports='\n'.join(ast.unparse(n) for n in ast.parse(relay.read_text()).body if isinstance(n,(ast.Import,ast.ImportFrom)))
 basic=imports+"\nprint('RELAY_IMPORTS_COMPLETE',flush=True)\n"
 site="""import sys,pwd,traceback,os,json
real=pwd.getpwuid
def observe(uid):
 print('NSS_BEGIN '+json.dumps(dict(pid=os.getpid(),uid=uid,stack=traceback.format_stack(limit=12))),flush=True)
 try:return real(uid)
 finally:print('NSS_END',flush=True)
pwd.getpwuid=observe
print('BEFORE_SITE_MAIN',flush=True)
import site
site.main()
print('AFTER_SITE_MAIN',flush=True)
"""
 libc="""import ctypes,os
libc=ctypes.CDLL(None,use_errno=True)
libc.getpwuid.argtypes=[ctypes.c_uint];libc.getpwuid.restype=ctypes.c_void_p
print('LIBC_GETPWUID_BEGIN',os.getpid(),flush=True)
assert libc.getpwuid(os.getuid())
print('LIBC_GETPWUID_END',flush=True)
"""
 results=[]
 for name,flags,source in [('default-site-control',[],basic),('relay-no-site',['-S'],basic),('explicit-site-control',['-S'],site),('libc-passwd-control',['-S'],libc)]:
  trace=Path('/tmp/'+name+'.trace')
  command=['/usr/bin/strace','-f','-ttt','-e','trace=network,write,execve','-o',str(trace),'/usr/bin/python3',*flags,'-u','-c',source]
  proc=subprocess.run(command,env={'LANG':'C','LC_ALL':'C','TZ':'UTC'},capture_output=True,text=True,timeout=8)
  assert proc.returncode==0,(name,proc.returncode,proc.stderr)
  raw=trace.read_text();calls=[line for line in raw.splitlines() if re.match(r'^\d+\s+[\d.]+\s+connect\(',line)]
  sockets=[line for line in raw.splitlines() if re.match(r'^\d+\s+[\d.]+\s+socket\(',line)]
  if name=='relay-no-site':assert calls==sockets==[],(name,calls,sockets)
  else:
   assert len(calls)==len(sockets)==2,(name,calls,sockets)
   assert all('sa_family=AF_UNIX' in line and 'sun_path="/var/run/nscd/socket"' in line and '= -1 ENOENT' in line for line in calls)
   assert all('socket(AF_UNIX,' in line for line in sockets)
   assert len({line.split()[0] for line in calls})==1
  if name=='default-site-control':assert raw.rindex('connect(')<raw.index('RELAY_IMPORTS_COMPLETE')
  if name=='explicit-site-control':
   stack=json.loads(next(line[len('NSS_BEGIN '):] for line in proc.stdout.splitlines() if line.startswith('NSS_BEGIN ')))
   assert stack['uid']==65534 and str(stack['pid'])==calls[0].split()[0]
   assert all(any(op in frame for frame in stack['stack']) for op in ['main','getuserbase','expanduser','observe'])
   assert raw.index('NSS_BEGIN')<raw.index('connect(')<raw.rindex('connect(')<raw.index('NSS_END')
  if name=='libc-passwd-control':assert raw.index('LIBC_GETPWUID_BEGIN')<raw.index('connect(')<raw.rindex('connect(')<raw.index('LIBC_GETPWUID_END')
  results.append(dict(name=name,command=command,exit_code=proc.returncode,stdout=proc.stdout,stderr=proc.stderr,trace=raw,connect_calls=calls))
 return dict(python=sys.version,uid=os.getuid(),status=status,nsswitch=Path('/etc/nsswitch.conf').read_text(),libc=subprocess.check_output(['ldd','--version'],text=True),results=results,passed=True)


def main():
 p=argparse.ArgumentParser();p.add_argument('--inside',action='store_true');p.add_argument('--relay',type=Path);p.add_argument('--evidence',type=Path);a=p.parse_args()
 if a.inside:print(json.dumps(inside(a.relay)));return
 assert a.evidence and a.evidence.is_absolute()
 a.evidence.mkdir(parents=True,exist_ok=False);here=Path(__file__).resolve().parent
 # Assert the actual launch uses -S. This does not replace black-box syscall
 # controls above or the full unchanged connect audit in every joint case.
 tree=ast.parse((here/'joint_init.py').read_text())
 launches=[n for n in ast.walk(tree) if isinstance(n,ast.Call) and isinstance(n.func,ast.Attribute) and n.func.attr=='Popen']
 assert len(launches)==1
 argv=launches[0].args[0].elts;python=next(i for i,n in enumerate(argv) if isinstance(n,ast.Constant) and n.value=='/usr/bin/python3')
 assert [n.value for n in argv[python+1:python+4]]==['-S','-u','-c']
 command=['docker','run','--rm','--network','none','--cap-drop','ALL','--security-opt','no-new-privileges:true','--read-only','--user','65534:65534',
          '--memory','128m','--memory-swap','128m','--cpus','1','--pids-limit','128','--ulimit','nofile=256:256','--tmpfs','/tmp:rw,noexec,nosuid,size=4m',
          '-v',str(Path(__file__).resolve())+':/check.py:ro','-v',str(here/'joint_bootstrap.py')+':/relay.py:ro',
          '--entrypoint','/usr/bin/python3','devpilot-scheduler-fixture','-S','-B','/check.py','--inside','--relay','/relay.py']
 result=subprocess.run(command,capture_output=True,text=True,timeout=45)
 (a.evidence/'command.json').write_text(json.dumps(dict(command=command,exit_code=result.returncode),indent=2)+'\n')
 (a.evidence/'stdout.log').write_text(result.stdout);(a.evidence/'stderr.log').write_text(result.stderr)
 assert result.returncode==0,(result.returncode,result.stderr)
 parsed=json.loads(result.stdout);assert parsed['passed']
 (a.evidence/'result.json').write_text(json.dumps(parsed,indent=2)+'\n')
 print(json.dumps(dict(passed=True,cases=[r['name'] for r in parsed['results']],network='none',all_caps_zero=True)))


if __name__=='__main__':main()
