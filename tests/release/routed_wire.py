"""Kernel packet test for the outer router. All three endpoints have network=none.

Uses a disposable veth in place of a physical NIC; no production PCI check is
overridden. The existing approved-https workflow tests the inner Permit path.
"""
import json
from pathlib import Path
import subprocess
import sys
import time
from contextlib import ExitStack

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'runtime/private'))
import uplink

IMAGE = 'devpilot-private-runtime'
TARGET4 = '93.184.216.34'
TARGET6 = '2001:4860:ffff::34'
HOST = 'a.fixture.example.com'


def docker(*args, data=None):
    return subprocess.run(['docker', *args], input=data, stdout=subprocess.PIPE,
                          check=True, timeout=30).stdout


def inside(cid, *args, data=None):
    return docker('exec', '-i', cid, *args, data=data)


def wire(left, lname, right, rname):
    docker('run', '--rm', '--network', 'container:' + left, '--pid', 'container:' + right,
           '--cap-drop', 'ALL', '--cap-add', 'NET_ADMIN', '--security-opt', 'no-new-privileges:true',
           '--entrypoint', 'sh', IMAGE, '-ec', 'ip link add "$1" type veth peer name "$2"; ip link set "$2" netns 1',
           'wire', lname, rname)
    inside(left, 'ip', 'link', 'set', lname, 'up')
    inside(right, 'ip', 'link', 'set', rname, 'up')


def test():
    with ExitStack() as cleanup:
        nodes = []
        for index in range(3):
            cid = docker('run', '-d', '--rm', '--network', 'none', '--cap-drop', 'ALL',
                         '--cap-add', 'NET_ADMIN', '--security-opt', 'no-new-privileges:true',
                         '--sysctl', 'net.ipv6.conf.all.disable_ipv6=0',
                         '--sysctl', 'net.ipv4.ip_forward=' + str(int(index == 1)),
                         '--sysctl', 'net.ipv6.conf.all.forwarding=' + str(int(index == 1)),
                         '--read-only', '--tmpfs', '/tmp:rw,nosuid,noexec,size=4m',
                         '--tmpfs', '/var/lib/postgresql:ro', '--entrypoint', 'sleep', IMAGE, 'infinity').decode().strip()
            cleanup.callback(docker, 'rm', '-f', cid)
            nodes.append(cid)
        client, router, server = nodes
        wire(client, 'eth0', router, 'dp1')
        wire(router, 'uplink0', server, 'edge0')
        for cid, dev, v4, v6 in [(client, 'eth0', '198.18.1.2', 'fd00:1::2'),
                                  (router, 'dp1', '198.18.1.1', 'fd00:1::1'),
                                  (router, 'uplink0', '198.18.2.1', 'fd00:2::1'),
                                  (server, 'edge0', '198.18.2.2', 'fd00:2::2')]:
            inside(cid, 'ip', 'addr', 'add', v4 + '/30', 'dev', dev)
            inside(cid, 'ip', '-6', 'addr', 'add', v6 + '/64', 'dev', dev, 'nodad')
        # Permanent upstream neighbors are also required on the real dedicated
        # link: local IPv6 neighbor discovery is intentionally default-dropped.
        for cid, dev, peer, pdev, address in [(router,'uplink0',server,'edge0','fd00:2::2'),
                                                (server,'edge0',router,'uplink0','fd00:2::1'),
                                                (client,'eth0',router,'dp1','fd00:1::1'),
                                                (router,'dp1',client,'eth0','fd00:1::2')]:
            mac = json.loads(inside(peer,'ip','-j','link','show',pdev))[0]['address']
            inside(cid,'ip','-6','neigh','replace',address,'lladdr',mac,'nud','permanent','dev',dev)
        for ip in ('192.0.2.53', TARGET4, TARGET6):
            v6 = ':' in ip
            af = '-6' if v6 else '-4'
            dest = ip + ('/128' if v6 else '/32')
            inside(server, 'ip', af, 'addr', 'add', dest, 'dev', 'lo', *(['nodad'] if v6 else []))
            for cid, via in [(router, 'fd00:2::2' if v6 else '198.18.2.2'),
                             (client, 'fd00:1::1' if v6 else '198.18.1.1')]:
                inside(cid, 'ip', af, 'route', 'add', dest, 'via', via)
        for cid in nodes:
            for af in ('-4','-6'):
                if json.loads(inside(cid,'ip',af,'-j','route','show','default')):
                    raise RuntimeError('unexpected upstream')
        receiver = (ROOT / 'tests/scheduler/joint_receiver.py').read_text()
        docker('exec','-d',server,'python3','-c',receiver.replace('JOINT_MODE',repr('allow')).replace('JOINT_FAMILY','4'))
        for _ in range(50):
            if subprocess.run(['docker','exec',server,'test','-s','/tmp/fixture.crt']).returncode == 0:
                break
            time.sleep(.1)
        cert = inside(server,'cat','/tmp/fixture.crt')
        inside(client,'sh','-c','cat > /tmp/roots',data=cert)
        request = dict(approval=dict(url='https://' + HOST + '/fixture/check'),denied_prefixes=['10.0.0.0/8','fd00::/8'])
        inside(client,'sh','-c','cat > /tmp/request',data=json.dumps(request).encode())
        for selected, other in [(TARGET4,TARGET6),(TARGET6,TARGET4)]:
            config = dict(mode='private-approved',target_ip=selected,approval=dict(addresses=[selected]),
                          routed_uplink=dict(pci_slot='0000:01:00.0',mac='02:00:00:00:00:01'))
            policy = uplink.rules(config)
            if selected == TARGET6:
                policy = 'delete table inet dp_uplink\n' + policy
            inside(router,'nft','-f','-',data=policy.encode())
            review = json.loads(inside(client,'/toolbin/privateapproval','review','--request-file','/tmp/request'))
            if review['host'] != HOST or review['addresses'] != [TARGET4]:
                raise RuntimeError('wrong DNS wire')
            script = """
import socket,ssl,sys
target,other,host = sys.argv[1:]
for addr,port in [(other,443),(target,8080)]:
    try: socket.create_connection((addr,port),.4).close()
    except TimeoutError: continue
    raise RuntimeError('unexpected connection or wrong refusal')
ctx=ssl.create_default_context(cafile='/tmp/roots')
with socket.create_connection((target,443),2) as raw, ctx.wrap_socket(raw,server_hostname=host) as sock:
    sock.sendall(('GET /fixture/check HTTP/1.1\\r\\nHost: '+host+'\\r\\nConnection: close\\r\\n\\r\\n').encode())
    if sock.makefile('rb').readline().strip()!=b'HTTP/1.1 200 OK': raise RuntimeError('wrong HTTPS response')
"""
            inside(client,'python3','-c',script,selected,other,HOST)
        print('PASS: no-upstream TCP DNS review, fixed-literal TLS on both families, sibling literal and alternate port dropped')


if __name__ == '__main__':
    test()
