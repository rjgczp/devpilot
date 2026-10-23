#!/usr/bin/env python3
"""Independent no-upstream witness; fixed data only, never a target proxy."""
import base64
import json
import os
import socket
import ssl
import struct
import subprocess
import threading
import time

MODE=JOINT_MODE
FAMILY=JOINT_FAMILY


lock = threading.Lock()


def event(kind, **values):
    with lock:
        print(json.dumps(dict(kind=kind, monotonic_ns=time.monotonic_ns(), **values)), flush=True)


subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                '-subj', '/CN=wrong.fixture.example.com' if MODE=='tls-invalid' else '/CN=a.fixture.example.com', '-addext', 'subjectAltName=DNS:wrong.fixture.example.com' if MODE=='tls-invalid' else 'subjectAltName=DNS:a.fixture.example.com',
                '-keyout', '/tmp/fixture.key', '-out', '/tmp/fixture.crt'], check=True, capture_output=True)
tls = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
tls.load_cert_chain('/tmp/fixture.crt', '/tmp/fixture.key')
tls.set_servername_callback(lambda sock, name, ctx: setattr(sock, 'fixture_sni', name))


def connection(conn, port):
    conn.settimeout(2)
    local, peer = conn.getsockname(), conn.getpeername()
    event('accept', target=local[0], port=port, peer=peer[0])
    try:
        if port == 443:
            conn = tls.wrap_socket(conn, server_side=True)
        if port == 53:
            # The original kernel uses one bounded TCP session for A/AAAA and
            # alias traversal. This resolver has no forwarding path.
            def readn(n):
                out=b''
                while len(out)<n:
                    x=conn.recv(n-len(out))
                    if not x:return None
                    out+=x
                return out
            queries=0
            while True:
                size=readn(2)
                if size is None:return
                n=struct.unpack('!H',size)[0];assert 12<=n<=512
                query=readn(n)
                if query is None:return
                end=12
                while query[end]:end+=query[end]+1
                end+=1;typ=struct.unpack('!H',query[end:end+2])[0]
                assert typ in (1,28) and query[end+2:end+4]==b'\x00\x01'
                queries+=1;rr=b'';count=0
                if MODE=='rebinding' and typ==28:
                    address='::1';rtype=28
                elif MODE=='ipv6-poison' and typ==28:
                    address='64:ff9b::5db8:d822';rtype=28
                else:
                    address='93.184.216.34' if FAMILY==4 else '2001:4860:ffff::34';rtype=1 if FAMILY==4 else 28
                if typ==rtype:
                    packed=socket.inet_pton(socket.AF_INET if rtype==1 else socket.AF_INET6,address)
                    rr=b'\xc0\x0c'+struct.pack('!HHIH',rtype,1,1,len(packed))+packed;count=1
                answer=query[:2]+b'\x81\x80\x00\x01'+struct.pack('!HHH',count,0,0)+query[12:end+4]+rr
                conn.sendall(struct.pack('!H',len(answer))+answer)
                event('dns',target=local[0],port=53,qtype=typ,sequence=queries,mode=MODE)
            return
        data = b''
        while b'\r\n\r\n' not in data and len(data) < 4096:
            chunk = conn.recv(512)
            if not chunk:
                return
            data += chunk
        event('get', target=local[0], port=port, sni=getattr(conn, 'fixture_sni', None),
              is_get=data.startswith(b'GET /fixture/') and b' HTTP/1.1\r\n' in data, header_only=b'Content-Length:' not in data and b'Transfer-Encoding:' not in data,
              credentials=any(k in data.lower() for k in (b'cookie:', b'authorization:', b'proxy-authorization:')))
        if MODE in ['tls-stop','tracer-stopped','tracer-crashed','supervisor-killed','stop-ack-lost','journal-collision']:
            event('held_tls',target=local[0]);conn.settimeout(.1)
            until=time.monotonic()+30
            while time.monotonic()<until:
                try:
                    if not conn.recv(16):event('closed_tls',target=local[0]);return
                except socket.timeout:continue
            return
        conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 1048576\r\nConnection: close\r\n\r\n')
        # No body is supplied: the approved Header-only engine must finish and
        # close immediately, rather than waiting for/draining that declared body.
        if conn.recv(1)==b'':event('header_only_close',target=local[0],port=port)
    except (OSError, ValueError):
        pass
    finally:
        conn.close()


def serve(sock,port):
    while True:
        conn, _ = sock.accept()
        threading.Thread(target=connection, args=(conn, port), daemon=True).start()


def udp(sock):
    while True:
        data,peer=sock.recvfrom(512)
        event('udp',target=sock.getsockname()[0],port=53,peer=peer[0])
        sock.sendto(data,peer)


def listener(family, port, datagram=False):
    sock = socket.socket(family,socket.SOCK_DGRAM if datagram else socket.SOCK_STREAM)
    if family == socket.AF_INET6:
        sock.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
    sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    sock.bind(('::' if family == socket.AF_INET6 else '0.0.0.0', port))
    if datagram:
        threading.Thread(target=udp,args=(sock,),daemon=True).start()
    else:
        sock.listen(64)
        threading.Thread(target=serve,args=(sock,port),daemon=True).start()


for family in (socket.AF_INET, socket.AF_INET6):
    for port in (53, 80, 443, 8080):
        listener(family,port)
    listener(family,53,True)
event('ready', certificate=open('/tmp/fixture.crt').read(), uid=os.getuid())
threading.Event().wait()
