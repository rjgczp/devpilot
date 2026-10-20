#!/usr/bin/env python3
"""Fail closed Linux capability preflight; run only in a fresh network-none NS."""
import json
import os
import pathlib
import shutil
import socket
import subprocess
import threading
import time


def checked(*args, input=None):
    return subprocess.run(args, input=input, text=True, capture_output=True, check=True).stdout.strip()


def main():
    assert os.uname().sysname == 'Linux'
    for name in ('ip', 'nft', 'conntrack', 'setpriv', 'strace', 'tcpdump', 'python3', 'openssl', 'psql'):
        assert shutil.which(name), 'missing required tool: ' + name
    links = json.loads(checked('ip', '-j', 'link', 'show'))
    assert not any(x['ifname'] != 'lo' and 'UP' in x.get('flags', []) for x in links), 'upstream link'
    for family in ('-4', '-6'):
        routes = json.loads(checked('ip', family, '-j', 'route', 'show', 'table', 'all'))
        assert not any(x.get('dst') == 'default' for x in routes), 'upstream route'
    assert pathlib.Path('/proc/sys/net/ipv6/conf/all/disable_ipv6').read_text().strip() == '0'
    checked('ip', 'link', 'add', 'fixture', 'type', 'dummy')
    checked('ip', 'link', 'set', 'fixture', 'up')
    checked('ip', 'addr', 'add', '93.184.216.34/32', 'dev', 'fixture')
    checked('ip', '-6', 'addr', 'add', '2001:4860:ffff::1/128', 'dev', 'fixture', 'nodad')
    for family, address in ((socket.AF_INET, '93.184.216.34'), (socket.AF_INET6, '2001:4860:ffff::1')):
        s = socket.socket(family)
        s.bind((address, 0))
        s.close()
    # Kernel timed set expires independently of a living supervisor process.
    # Both directions check the same permit; no ESTABLISHED bypass exists.
    rules = '''table inet scheduler_preflight {
 set targets { type ipv4_addr . inet_service; flags timeout; }
 chain out { type filter hook output priority -200; policy drop;
   ip daddr . tcp dport @targets accept
   ip saddr . tcp sport @targets accept
 }
 chain in { type filter hook input priority -200; policy drop;
   ip daddr . tcp dport @targets accept
   ip saddr . tcp sport @targets accept
 }
}'''
    checked('nft', '-f', '-', input=rules)
    checked('nft', 'add', 'element', 'inet', 'scheduler_preflight', 'targets', '{ 93.184.216.34 . 80 timeout 1500ms }')
    server = socket.socket()
    server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    server.bind(('93.184.216.34', 80))
    server.listen(1)
    received = []
    def receive():
        conn, _ = server.accept()
        conn.settimeout(3)
        try:
            while True:
                b = conn.recv(32)
                if not b:
                    break
                received.append((time.monotonic(), b.decode()))
        except TimeoutError:
            pass
        finally:
            conn.close()
    t = threading.Thread(target=receive)
    t.start()
    client = socket.socket()
    client.settimeout(1)
    client.connect(('93.184.216.34', 80))
    client.sendall(b'before')
    deadline = time.monotonic() + 3
    while not received and time.monotonic() < deadline:
        time.sleep(.01)
    assert received and received[0][1] == 'before', 'positive control failed'
    time.sleep(1.7)
    client.sendall(b'after')
    client.close()
    t.join(5)
    server.close()
    assert not t.is_alive() and ''.join(x[1] for x in received) == 'before', 'expired established flow survived'
    denied = subprocess.run(['setpriv', '--reuid=65534', '--regid=65534', '--clear-groups', '--bounding-set=-all', '--inh-caps=-all', '--ambient-caps=-all', '--no-new-privs', 'python3', '-c', '''import pathlib,subprocess
s=pathlib.Path('/proc/self/status').read_text()
assert 'CapEff:\t0000000000000000' in s and 'NoNewPrivs:\t1' in s
assert subprocess.run(['nft','flush','ruleset'],capture_output=True).returncode != 0
assert subprocess.run(['ip','route','add','default','via','93.184.216.34'],capture_output=True).returncode != 0
assert not pathlib.Path('/var/run/docker.sock').exists()
'''], capture_output=True)
    assert denied.returncode == 0, 'probe privilege separation failed'
    print(json.dumps({'linux': os.uname().release, 'network_namespace': os.readlink('/proc/self/ns/net'), 'upstream': False, 'ipv6': True, 'kernel_permit_expiry_cuts_established_flow': True, 'unprivileged_probe_cannot_change_network': True, 'nft': checked('nft', '--version'), 'rules': checked('nft', 'list', 'ruleset')}, sort_keys=True))


if __name__ == '__main__':
    main()
