#!/usr/bin/env python3
"""Actual Linux packet tests for the host adapter, entirely network-none."""
import datetime
import importlib.util
import json
from pathlib import Path
import subprocess
import select
import os
import sys
import time
from contextlib import ExitStack
ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'runtime/private_lite'))
from egress import HostFilter
spec=importlib.util.spec_from_file_location('wire',ROOT/'tests/release/routed_wire.py')
w=importlib.util.module_from_spec(spec);spec.loader.exec_module(w)
w.IMAGE='devpilot-private-lite-runtime'

def deadline(seconds=34):
 return (datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=seconds)).isoformat()

def line(proc,seconds=6):
 end=time.monotonic()+seconds;data=bytearray()
 while time.monotonic()<end and len(data)<4096:
  if not select.select([proc.stdout],[],[],max(0,end-time.monotonic()))[0]:break
  b=os.read(proc.stdout.fileno(),1)
  if not b:break
  if b==b'\n':return bytes(data)
  data+=b
 raise RuntimeError('bounded packet actor readiness failed')

CLIENT='''import socket,sys
ip,port,want=sys.argv[1:];sock=socket.socket(socket.AF_INET6 if ':' in ip else socket.AF_INET,socket.SOCK_STREAM);sock.settimeout(.35)
try:
 sock.connect((ip,int(port)));sock.sendall(b'x');got=sock.recv(1)==b'x'
except (TimeoutError,OSError):got=False
assert got==(want=='allow'),(ip,port,want,got)
'''
SERVER='''import socket,threading,time
ready=threading.Barrier(7)
def serve(family,port):
 s=socket.socket(family,socket.SOCK_STREAM);s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 if family==socket.AF_INET6:s.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
 s.bind(('::' if family==socket.AF_INET6 else '0.0.0.0',port));s.listen();ready.wait()
 def echo(c):
  try:
   while True:
    b=c.recv(1)
    if not b:break
    c.sendall(b)
  finally:c.close()
 while True:
  c,_=s.accept();threading.Thread(target=echo,args=(c,),daemon=True).start()
for family in [socket.AF_INET,socket.AF_INET6]:
 for port in [53,443,8080]:threading.Thread(target=serve,args=(family,port),daemon=True).start()
ready.wait();print('ready',flush=True);time.sleep(180)
'''

def test(dest):
 dest.mkdir(parents=True,exist_ok=False)
 records=[]
 for failure in ['policy','guard','expiry','revoke','close']:
  with ExitStack() as cleanup:
   nodes=[]
   for index in range(3):
    cid=w.docker('run','-d','--rm','--network','none','--cap-drop','ALL','--cap-add','NET_ADMIN','--read-only','--security-opt','no-new-privileges:true','--tmpfs','/tmp:rw,noexec,nosuid,size=4m','--tmpfs','/var/lib/postgresql:ro','--sysctl','net.ipv6.conf.all.disable_ipv6=0','--sysctl','net.ipv4.ip_forward='+str(int(index==1)),'--sysctl','net.ipv6.conf.all.forwarding='+str(int(index==1)),'--entrypoint','sleep',w.IMAGE,'infinity').decode().strip()
    cleanup.callback(w.docker,'rm','-f',cid);nodes.append(cid)
   client,host,server=nodes
   w.wire(client,'eth0',host,'lite0');w.wire(host,'wan0',server,'edge0')
   for cid,dev,ip in [(client,'eth0','198.18.2.2'),(host,'lite0','198.18.2.1'),(host,'wan0','198.18.3.1'),(server,'edge0','198.18.3.2')]:w.inside(cid,'ip','addr','add',ip+'/30','dev',dev)
   targets=['1.1.1.1','93.184.216.34','93.184.216.35','169.254.169.254','10.0.0.9','11.0.0.9']
   for ip in targets:
    w.inside(server,'ip','addr','add',ip+'/32','dev','lo')
    w.inside(host,'ip','route','add',ip+'/32','via','198.18.3.2')
    w.inside(client,'ip','route','add',ip+'/32','via','198.18.2.1')
   w.inside(server,'ip','route','add','198.18.2.0/30','via','198.18.3.1')
   for cid,dev,ip in [(client,'eth0','fd00:2::2'),(host,'lite0','fd00:2::1'),(host,'wan0','fd00:3::1'),(server,'edge0','fd00:3::2')]:w.inside(cid,'ip','-6','addr','add',ip+'/64','dev',dev,'nodad')
   w.inside(server,'ip','-6','addr','add','2001:4860:ffff::34/128','dev','lo','nodad')
   w.inside(client,'ip','-6','route','add','2001:4860:ffff::34/128','via','fd00:2::1')
   w.inside(host,'ip','-6','route','add','2001:4860:ffff::34/128','via','fd00:3::2')
   w.inside(server,'ip','-6','route','add','fd00:2::/64','via','fd00:3::1')
   for cid,dev,peer,pdev,address in [(client,'eth0',host,'lite0','fd00:2::1'),(host,'lite0',client,'eth0','fd00:2::2'),(host,'wan0',server,'edge0','fd00:3::2'),(server,'edge0',host,'wan0','fd00:3::1')]:
    mac=json.loads(w.inside(peer,'ip','-j','link','show',pdev))[0]['address']
    w.inside(cid,'ip','-6','neigh','replace',address,'lladdr',mac,'nud','permanent','dev',dev)
   p=subprocess.Popen(['docker','exec',server,'python3','-u','-c',SERVER],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   cleanup.callback(p.terminate);assert line(p)==b'ready'
   host_server=subprocess.Popen(['docker','exec',host,'python3','-u','-c',SERVER],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   cleanup.callback(host_server.terminate);assert line(host_server)==b'ready'
   w.inside(host,'python3','-c',CLIENT,'127.0.0.1','443','allow')
   def attempt(ip,port,want):
    w.docker('run','--rm','--network','container:'+client,'--user','65534:65534','--cap-drop','ALL','--read-only','--security-opt','no-new-privileges:true','--entrypoint','python3',w.IMAGE,'-c',CLIENT,ip,str(port),want)
   # Calibrate the paths before installing the filter: refusal cannot pass
   # merely because IPv6 neighbor discovery or the test receiver is broken.
   for ip in ['2001:4860:ffff::34','93.184.216.35','198.18.2.1']:attempt(ip,443,'allow')
   f=HostFilter(lambda *a,**kw:w.inside(host,*a,**kw),'test','lite0','198.18.2.2','1.1.1.1','93.184.216.34');f.install()
   attempt('93.184.216.34',443,'deny')
   f.grant(deadline(),True);attempt('93.184.216.34',443,'allow');attempt('1.1.1.1',53,'allow')
   for ip,port in [('93.184.216.35',443),('169.254.169.254',443),('10.0.0.9',443),('11.0.0.9',443),('1.1.1.1',443),('93.184.216.34',8080),('2001:4860:ffff::34',443),('198.18.2.1',443),('198.18.3.1',443)]:attempt(ip,port,'deny')
   # Reuse an already established TCP connection across policy deletion/revoke.
   persistent='''import socket,sys
s=socket.create_connection(('93.184.216.34',443),2);s.settimeout(.5);s.sendall(b'x');assert s.recv(1)==b'x';print('ready',flush=True);sys.stdin.readline();s.sendall(b'x')
try:s.recv(1);raise RuntimeError('established connection survived revoke')
except TimeoutError:print('blocked',flush=True)
'''
   live=subprocess.Popen(['docker','run','--rm','-i','--network','container:'+client,'--user','65534:65534','--cap-drop','ALL','--read-only','--security-opt','no-new-privileges:true','--entrypoint','python3',w.IMAGE,'-u','-c',persistent],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
   assert line(live)==b'ready'
   if failure in ('policy','guard'):w.inside(host,'nft','delete','table','inet',f.tables[int(failure=='guard')])
   elif failure=='revoke':f.revoke()
   elif failure=='close':
    f.close()
    try:f.grant(deadline(),True)
    except ValueError:pass
    else:raise RuntimeError('closed host admission reopened')
   else:f.grant(deadline(3),True);time.sleep(1.1)
   live.stdin.write(b'go\n');live.stdin.close();assert line(live)==b'blocked';assert live.wait(timeout=5)==0
   attempt('93.184.216.34',443,'deny')
   if failure in ('policy','guard'):
    try:f.grant(deadline(),True)
    except Exception:pass
    else:raise RuntimeError('lost table renewed permission')
   records.append(dict(case=failure,positive_dns_https=True,unauthorized_private_metadata_management_ipv6_denied=True,established_and_new_denied=True,no_upstream=True))
   # Stop server before ExitStack removes its disconnected namespace.
   # Namespace teardown kills the owned receiver; this is test cleanup only.
 dest.joinpath('packets.json').write_text(json.dumps(records,indent=2)+'\n')
 print('PASS Private-Lite host packet boundaries',flush=True)

if __name__=='__main__':test(Path(sys.argv[1]))
