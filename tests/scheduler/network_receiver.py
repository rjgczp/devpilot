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


lock = threading.Lock()


def event(kind, **values):
    with lock:
        print(json.dumps(dict(kind=kind, monotonic_ns=time.monotonic_ns(), **values)), flush=True)


subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
                '-subj', '/CN=a.fixture.example.com', '-addext', 'subjectAltName=DNS:a.fixture.example.com',
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
            size = conn.recv(2)
            if len(size) != 2:
                return
            n = struct.unpack('!H', size)[0]
            query = b''
            while len(query) < n:
                query += conn.recv(n-len(query))
            if len(query) < 12 or len(query) > 512:
                return
            # Fixed answer for the local fixture. No forwarding/upstream exists.
            answer = query[:2] + b'\x81\x80\x00\x01\x00\x01\x00\x00\x00\x00' + query[12:] + b'\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x01\x00\x04' + socket.inet_aton('93.184.216.34')
            conn.sendall(struct.pack('!H', len(answer)) + answer)
            event('dns', target=local[0], port=53)
            return
        data = b''
        while b'\r\n\r\n' not in data and len(data) < 4096:
            chunk = conn.recv(512)
            if not chunk:
                return
            data += chunk
        if data.startswith(b'GET /fixture/stream HTTP/1.1\r\n'):
            event('stream_open', target=local[0], port=port, sni=getattr(conn,'fixture_sni',None))
            conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n')
            conn.settimeout(.2)
            until=time.monotonic()+35
            pending=b''
            while time.monotonic()<until:
                try:chunk=conn.recv(128)
                except socket.timeout:continue
                if not chunk:return
                pending+=chunk
                while b'\n' in pending:
                    frame,pending=pending.split(b'\n',1)
                    assert frame in (b'before-stop',b'after-stop')
                    event('stream_frame',frame=frame.decode(),target=local[0])
            return
        event('get', target=local[0], port=port, sni=getattr(conn, 'fixture_sni', None),
              is_get=data.startswith(b'GET /fixture HTTP/1.1\r\n'),
              credentials=any(k in data.lower() for k in (b'cookie:', b'authorization:', b'proxy-authorization:')))
        conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n')
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
