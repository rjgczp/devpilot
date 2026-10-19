#!/usr/bin/env python3
"""Independent DNS/HTTP/TLS endpoints in a namespace with no upstream route."""
import ipaddress
import json
import socket
import ssl
import struct
import threading
import time
from pathlib import Path

P = '93.184.216.34'
V6 = '2606:4700:4700::1111'
lock = threading.Lock()
rebound = False
events = Path('/evidence/events.jsonl')

def event(kind, **data):
    with lock:
        with events.open('a') as out:
            out.write(json.dumps(dict(kind=kind, monotonic=time.monotonic(), **data))+'\n')

def name(s):
    return b''.join(bytes([len(x)])+x.encode() for x in s.split('.'))+b'\0'

def rr(owner, typ, data):
    return name(owner)+struct.pack('!HHIH',typ,1,0,len(data))+data

def address(owner, ip):
    ip = ipaddress.ip_address(ip)
    return rr(owner, 1 if ip.version==4 else 28, ip.packed)

def readn(conn, n):
    out = b''
    while len(out)<n:
        b = conn.recv(n-len(out))
        if not b:
            raise EOFError()
        out += b
    return out

def dns(conn):
    global rebound
    try:
        while True:
            size = struct.unpack('!H',readn(conn,2))[0]
            q = readn(conn,size)
            pos, labels = 12, []
            while q[pos]:
                n=q[pos];pos+=1;labels.append(q[pos:pos+n].decode());pos+=n
            host='.'.join(labels);pos+=1
            typ, cls=struct.unpack('!HH',q[pos:pos+4]);question=q[12:pos+4]
            event('dns',host=host,qtype=typ)
            case=host.split('.')[0];answers=[];extra=[];rcode=0
            if case=='dns-timeout':
                time.sleep(3)
            if case=='dns-nxdomain': rcode=3
            elif case=='dns-servfail': rcode=2
            elif case in ('dns-empty','dns-truncated','dns-id','dns-question','dns-trailing'):
                pass
            elif case=='oversize':
                conn.sendall(struct.pack('!H',17000)+bytes(17000));return
            elif case=='mixed' and typ==1:
                answers=[address(host,P),address(host,'10.0.0.1')]
            elif case=='mixed6':
                answers=[address(host,P)] if typ==1 else [address(host,'fd00::1')]
            elif case=='additional':
                answers=[address(host,P)] if typ==1 else []
                extra=[address('unrelated.example.com','169.254.169.254')]
            elif case=='reserved-alias':answers=[rr(host,5,name('target.internal'))]
            elif case in ('cname','cname-tls'):
                answers=[rr(host,5,name('canonical.example.com'))]
                if typ==1: answers.append(address('canonical.example.com',P))
            elif case=='inconsistent':
                target=('a' if typ==1 else 'b')+'.example.com'
                answers=[rr(host,5,name(target))]
                if typ==1:answers.append(address(target,P))
            elif case=='aliases' or case.startswith('alias-step-'):
                i=int(case.rsplit('-',1)[1])+1 if case.startswith('alias-step-') else 1
                answers=[rr(host,5,name(f'alias-step-{i}.example.com'))]
            elif case=='dns-chain-slow' or case.startswith('slowalias-step-'):
                time.sleep(.35)
                i=int(case.rsplit('-',1)[1])+1 if case.startswith('slowalias-step-') else 1
                answers=[rr(host,5,name(f'slowalias-step-{i}.example.com'))]
            elif case=='addresses-boundary' and typ==1:
                answers=[address(host,P)]+[address(host,f'93.184.216.{i}') for i in range(100,131)]
            elif case=='records-boundary':
                answers=[address(host,P if typ==1 else V6)]*64
            elif case=='addresses' and typ==1:
                answers=[address(host,f'93.184.216.{i}') for i in range(1,34)]
            elif case=='records':
                answers=[address(host,P if typ==1 else V6)]*65
            elif case=='total' or case.startswith('total-step-'):
                i=int(case.rsplit('-',1)[1])+1 if case.startswith('total-step-') else 1
                answers=[rr(host,5,name(f'total-step-{i}.example.com'))]
                extra=[rr('unused.example.com',16,(b'\xff'+b'x'*255)*58)]
            elif case=='dname':answers=[rr(host,39,name('elsewhere.example.com'))]
            elif case.startswith('blocked-'):
                blocked={'private':'10.0.0.1','metadata':'169.254.169.254','shared':'100.100.100.200','loopback':'127.0.0.1','mapped':'::ffff:8.8.8.8','nat64':'64:ff9b::a9fe:a9fe','6to4':'2002:0808:0808::1','teredo':'2001::1','isatap':'2606:4700::5efe:808:808','doc':'192.0.2.1','v6doc':'3fff::1','ula':'fd00::1'}[case[8:]]
                if typ==(1 if ':' not in blocked else 28):answers=[address(host,blocked)]
            elif case=='nat-app':
                if typ==28:answers=[address(host,'2001:4860:ffff::a9fe:a9fe')]
            elif case=='ipv6':
                if typ==28:answers=[address(host,V6)]
            elif case=='rebinding':
                if typ==1:answers=[address(host,'10.0.0.1' if rebound else P)]
            elif typ==1:
                ip={'refuse':'93.184.216.35','tcp-timeout':'93.184.216.37','tls-stall':'93.184.216.38'}.get(case,P)
                answers=[address(host,ip)]
            flags=0x8180|rcode
            if case=='dns-truncated':flags|=0x0200
            ident=q[:2] if case!='dns-id' else b'\xff\xff' if q[:2]!=b'\xff\xff' else b'\x00\x00'
            if case=='dns-question':question=name('wrong.example.com')+struct.pack('!HH',typ,cls)
            payload=ident+struct.pack('!HHHHH',flags,1,len(answers),0,len(extra))+question+b''.join(answers+extra)
            if case=='dns-trailing':payload+=b'x'
            conn.sendall(struct.pack('!H',len(payload))+payload)
            if case=='rebinding' and typ==28:
                rebound=True
                event('rebind',host=host)
    except (OSError,EOFError,ValueError,IndexError):
        pass
    finally:
        conn.close()

def context(cert):
    ctx=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    ctx.minimum_version=ssl.TLSVersion.TLSv1_2
    ctx.load_cert_chain('/fixtures/'+cert,'/fixtures/server.key')
    ctx.set_alpn_protocols(['http/1.1'])
    return ctx

normal=context('server.pem');expired=context('expired.pem');big=context('big.pem')
def sni(sock, host, ctx):
    event('sni',host=host)
    if host=='expired.example.com':sock.context=expired
    if host=='tls-big.example.com':sock.context=big
normal.set_servername_callback(sni)

def http(conn, ip, tls):
    event('tcp',ip=ip,tls=tls)
    try:
        if tls:
            # A raw TLS-stall target uses a dedicated DNS-selected IPv4 below.
            conn=normal.wrap_socket(conn,server_side=True)
        buf=b''
        while b'\r\n\r\n' not in buf and len(buf)<16384:
            b=conn.recv(4096)
            if not b:return
            buf+=b
        headers=buf.decode('ascii',errors='replace')
        host=next((x.split(':',1)[1].strip() for x in headers.split('\r\n') if x.lower().startswith('host:')), '')
        method=headers.split(' ',1)[0]
        event('http',host=host,ip=ip,method=method,credential=any(x in headers.lower() for x in ['authorization:','cookie:','proxy-authorization:']),encoding='accept-encoding: identity' in headers.lower())
        case=host.split('.')[0]
        if case.startswith('hold') or case=='slow':
            if case=='hold-pg':
                while not Path('/evidence/release-pg').exists():time.sleep(.01)
            else:
                time.sleep(8)
        if case=='malformed':conn.sendall(b'not HTTP\r\n\r\n');return
        if case=='header-big':conn.sendall(b'HTTP/1.1 200 OK\r\nX: '+b'x'*33000+b'\r\n\r\n');return
        if case in ('hints','flood','hints-big'):
            for _ in range(5 if case=='flood' else 4):
                conn.sendall(b'HTTP/1.1 103 Early Hints\r\n'+(b'X: '+b'x'*19000+b'\r\n' if case=='hints-big' else b'')+b'\r\n')
        if case=='flood-lf':
            conn.sendall(b'HTTP/1.1 103 Early Hints\n\n'*5)
        if case=='upgrade':conn.sendall(b'HTTP/1.1 101 Switching Protocols\r\nConnection: upgrade\r\nUpgrade: test\r\n\r\n');return
        code=int(case.split('-')[1]) if case.startswith('redirect-') else 200
        location=b'Location: http://internal.example.com/secret-sentinel\r\n' if code in (301,302,307,308) else b''
        if case=='chunked':
            conn.sendall(b'HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\nffffff\r\n'+b'x'*4096)
        else:
            encoding=b'Content-Encoding: gzip\r\n' if case=='gzip' else b''
            conn.sendall(f'HTTP/1.1 {code} fixture\r\n'.encode()+location+encoding+b'Content-Length: 999999999\r\n\r\n')
            if case in ('infinite','gzip'):
                for _ in range(64):conn.sendall(b'x'*4096)
    except (OSError,ssl.SSLError):
        pass
    finally:
        conn.close()

def listener(ip,port,callback):
    family=socket.AF_INET6 if ':' in ip else socket.AF_INET
    sock=socket.socket(family,socket.SOCK_STREAM);sock.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
    sock.bind((ip,port));sock.listen(128)
    def serve():
        while True:
            conn,_=sock.accept()
            threading.Thread(target=callback,args=(conn,),daemon=True).start()
    threading.Thread(target=serve,daemon=True).start()

listener('127.0.0.53',53,dns)
for ip in (P,V6,'169.254.169.254','fd00::1','93.184.216.36'):
    listener(ip,80,lambda c,ip=ip:http(c,ip,False))
listener(P,443,lambda c:http(c,P,True))
listener('93.184.216.38',443,lambda c:(event('tls-stall'),time.sleep(5),c.close()))
listener('127.0.0.54',8080,lambda c:(event('proxy'),c.close()))
Path('/evidence/fixture-ready').write_text('ready\n')
while True:time.sleep(60)
