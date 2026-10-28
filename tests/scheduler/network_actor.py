#!/usr/bin/env python3
"""Deliberate policy-bypass test child, NOT the approved HTTP execution kernel.

Trusted harness passes this source and bounded fixture actions on anonymous
stdin. It has no URL/config/DB volume or management channel. These socket calls
are independent firewall witnesses; they never replace internal/monitoring.
"""
import base64
import json
import os
import pathlib
import resource
import socket
import ssl
import struct
import subprocess
import sys
import time


def run(config):
    assert os.getuid() == 65534 and os.geteuid() == 65534
    status = pathlib.Path('/proc/self/status').read_text()
    fields = dict(line.split(':', 1) for line in status.splitlines() if ':' in line)
    assert all(fields[k].strip() == '0000000000000000' for k in ('CapEff', 'CapPrm', 'CapBnd', 'CapInh', 'CapAmb'))
    assert fields['NoNewPrivs'].strip() == '1'
    assert dict(os.environ) == {'LANG': 'C', 'LC_ALL': 'C', 'TZ': 'UTC'}
    assert resource.getrlimit(resource.RLIMIT_NOFILE) == (256, 256)
    assert pathlib.Path('/sys/fs/cgroup/memory.max').read_text().strip() == '134217728'
    assert pathlib.Path('/sys/fs/cgroup/pids.max').read_text().strip() == '128'
    quota, period = pathlib.Path('/sys/fs/cgroup/cpu.max').read_text().split()
    assert int(quota) == int(period)
    assert os.statvfs('/').f_flag & os.ST_RDONLY
    for secret in ('/var/run/docker.sock', '/run/containerd/containerd.sock', '/root/.aws/credentials', '/app/.env', '/workspace'):
        try:
            f = open(secret)
        except (FileNotFoundError, PermissionError, IsADirectoryError):
            continue
        f.close()
        raise AssertionError('secret or management mount accessible')
    admin = []
    if config.get('admin'):
        for cmd in (['/usr/sbin/nft', 'flush', 'ruleset'], ['/usr/sbin/ip', 'route', 'add', 'default', 'via', config['gateway']],
                    ['unshare', '--net', 'true'], ['unshare','--user','--map-root-user','--net','true'], ['nsenter', '--net=/proc/1/ns/net', 'true']):
            r = subprocess.run(cmd, capture_output=True)
            admin.append(dict(command=cmd, exit_code=r.returncode))
            assert r.returncode != 0
        try:
            socket.socket(socket.AF_INET, socket.SOCK_RAW, socket.IPPROTO_RAW)
        except PermissionError:
            pass
        else:
            raise AssertionError('raw socket capability')
    results = []
    for action in config.get('actions', []):
        ip, port = action['ip'], action['port']
        family = socket.AF_INET6 if ':' in ip else socket.AF_INET
        sock = socket.socket(family,socket.SOCK_DGRAM if action.get('udp') else socket.SOCK_STREAM)
        sock.settimeout(.35)
        start = time.monotonic_ns()
        connected = False
        success = False
        try:
            sock.connect((ip, port))
            connected = not action.get('udp')
            if action.get('udp'):
                sock.send(b'fixture-udp')
                success = sock.recv(512)==b'fixture-udp'
                # UDP connect is local bookkeeping, not receiver delivery.
                connected=success
            elif action.get('dns'):
                name = b'\x01a\x07fixture\x07example\x03com\x00'
                query = b'\x64\x64\x01\x00\x00\x01\x00\x00\x00\x00\x00\x00' + name + b'\x00\x01\x00\x01'
                sock.sendall(struct.pack('!H', len(query))+query)
                success = len(sock.recv(512)) > 2
            else:
                if port == 443 and action.get('tls'):
                    ctx = ssl.create_default_context(cadata=config['certificate'])
                    sock = ctx.wrap_socket(sock, server_hostname='a.fixture.example.com')
                sock.sendall(b'GET /fixture HTTP/1.1\r\nHost: a.fixture.example.com\r\nConnection: close\r\n\r\n')
                success = sock.recv(512).startswith(b'HTTP/1.1 200')
        except OSError:
            pass
        finally:
            sock.close()
        results.append(dict(ip=ip, port=port, connect_attempts=1, connected=connected, success=success,
                            started_ns=start, finished_ns=time.monotonic_ns()))
    return dict(uid=os.getuid(), caps_zero=True, no_new_privs=True, resources=True, no_credentials=True,
                namespace=os.readlink('/proc/self/ns/net'), admin_denials=admin, actions=results)
