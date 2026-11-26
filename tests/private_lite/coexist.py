#!/usr/bin/env python3
"""Private-Lite component coexistence and daemon-loss tests in owned Linux.

Outer Docker is NEVER restarted. The privileged test daemon has network=none,
private data/run tmpfs and a fresh bounded cgroup. No real target or SQL/receipt.
"""
import datetime
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT/'runtime/private_lite'))
from egress import HostFilter
from namespace import NamespaceExecutor
spec = importlib.util.spec_from_file_location('packets', ROOT/'tests/private_lite/network.py')
n = importlib.util.module_from_spec(spec); spec.loader.exec_module(n)
IMAGE = 'devpilot-lite-coexist:26.1.3'


def outer(*args, data=None, check=True, timeout=45):
    p = subprocess.run(['docker', *args], input=data, capture_output=True, timeout=timeout)
    if check and p.returncode:
        raise RuntimeError((args[:3], p.returncode, p.stderr.decode()[-1000:]))
    return p


def deadline():
    return (datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=34)).isoformat()


class Rig:
    def __init__(self, dest, live):
        self.dest, self.live = dest, live
        self.dest.mkdir()
        self.cg = 'dpl-lite-'+uuid.uuid4().hex[:16]
        self.cid = None
        self.executor = None
        self.records = []

    def record(self, name, **values):
        row = dict(name=name, **values)
        self.records.append(row)
        (self.dest/'results.json').write_text(json.dumps(self.records, indent=2)+'\n')
        print(json.dumps(row), flush=True)

    def boot(self):
        # Only a new cgroup is written, never another container's controllers.
        script = '''set -eu
cg=/sys/fs/cgroup/$1
test ! -e "$cg"
mkdir "$cg"
echo 805306368 > "$cg/memory.max"
echo 384 > "$cg/pids.max"
echo '200000 100000' > "$cg/cpu.max"
echo '+cpu +memory +pids' > "$cg/cgroup.subtree_control"
sleep infinity
'''
        self.cid = outer('create', '--name', self.cg, '--network', 'none', '--privileged', '--cgroupns', 'host',
                         '--read-only', '--memory=3g', '--memory-swap=3g', '--cpus=2', '--pids-limit=512',
                         '--tmpfs', '/var/lib/docker:rw,nosuid,size=1536m', '--tmpfs', '/run:rw,nosuid,size=32m',
                         '--tmpfs', '/tmp:rw,nosuid,size=16m', '-e', 'DOCKER_HOST=unix:///run/docker.sock',
                         '-v', str(self.dest)+':/evidence', IMAGE, '-ec', script, 'owned-cgroup', self.cg).stdout.decode().strip()
        outer('start', self.cid)
        self.start_daemon()
        export = subprocess.Popen(['docker', 'image', 'save', IMAGE], stdout=subprocess.PIPE)
        try:
            load = subprocess.run(['docker', 'exec', '-i', self.cid, 'docker', 'image', 'load'], stdin=export.stdout, capture_output=True, timeout=60)
        finally:
            export.stdout.close()
        if export.wait(timeout=5) != 0 or load.returncode:
            raise RuntimeError('owned image import failed')
        assert not json.loads(self.root('ip', '-j', 'route', 'show', 'default'))
        assert not json.loads(self.root('ip', '-6', '-j', 'route', 'show', 'default'))
        self.record('environment', no_upstream=True, live_restore=self.live,
                    docker=json.loads(self.inner('version', '--format', '{{json .Server}}')),
                    data_plane_only=True, receipts_inferred=False)

    def root(self, *args, data=None, timeout=45):
        return outer('exec', '-i', self.cid, *args, data=data, timeout=timeout).stdout

    def inner(self, *args, **kw):
        return self.root('docker', *args, **kw)

    def inside(self, cid, *args, **kw):
        return self.inner('exec', '-i', cid, *args, **kw)

    def start_daemon(self):
        cmd = 'dockerd --host=unix:///run/docker.sock --storage-driver=overlay2 --ipv6 --fixed-cidr-v6=fd70:100::/64 --default-address-pool=base=172.27.0.0/16,size=24 --cgroup-parent=/$1 --live-restore=$2 >> /evidence/daemon.log 2>&1 & echo $! > /run/test-daemon.pid'
        self.root('sh', '-ec', cmd, 'daemon', self.cg, str(self.live).lower())
        end = time.monotonic()+20
        while time.monotonic() < end:
            try:
                p = outer('exec', self.cid, 'docker', 'info', check=False, timeout=2)
                if p.returncode == 0:
                    return
            except subprocess.TimeoutExpired:
                pass
            time.sleep(.1)
        raise RuntimeError('bounded isolated daemon readiness')

    def stop_daemon(self):
        self.root('sh', '-ec', 'kill -TERM "$(cat /run/test-daemon.pid)"')
        end = time.monotonic()+20
        while time.monotonic() < end:
            if self.root('sh', '-c', 'test ! -e /run/docker.pid && echo down || true').strip() == b'down':
                return
            time.sleep(.1)
        raise RuntimeError('bounded daemon shutdown')

    def node(self, name, network='none', extra=()):
        return self.inner('run', '-d', '--name', name, '--network', network, '--cap-drop', 'ALL', '--cap-add', 'NET_ADMIN',
                          '--security-opt', 'no-new-privileges:true', '--read-only', '--memory=64m', '--memory-swap=64m',
                          '--cpus=.5', '--pids-limit=64', '--tmpfs', '/tmp:rw,size=4m', *extra,
                          '--entrypoint', 'sleep', IMAGE, 'infinity').decode().strip()

    def peer(self, node, local, remote):
        self.inner('run', '--rm', '--network', 'container:'+node, '--pid', 'host', '--cap-drop', 'ALL', '--cap-add', 'NET_ADMIN',
                   '--entrypoint', 'sh', IMAGE, '-ec', 'ip link add "$1" type veth peer name "$2"; ip link set "$2" netns 1', 'wire', local, remote)
        self.inside(node, 'ip', 'link', 'set', local, 'up'); self.root('ip', 'link', 'set', remote, 'up')

    def echo(self, node):
        # The actual receiver is bounded by its owned container/PID namespace.
        self.inside(node, 'sh', '-ec', 'python3 -u -c "$1" > /tmp/echo.log 2>&1 &', 'echo', n.SERVER)
        end = time.monotonic()+5
        while time.monotonic() < end:
            if b'ready' in self.inside(node, 'cat', '/tmp/echo.log'):
                return
            time.sleep(.05)
        raise RuntimeError('owned receiver readiness')

    def attempt(self, node, ip, port, want):
        self.inner('run', '--rm', '--network', 'container:'+node, '--user', '65534:65534', '--cap-drop', 'ALL', '--read-only',
                   '--memory=48m', '--pids-limit=32', '--entrypoint', 'python3', IMAGE, '-c', n.CLIENT, ip, str(port), want)

    def nat(self):
        gateway = next(v['Gateway'] for v in json.loads(self.inner('network', 'inspect', 'bridge'))[0]['IPAM']['Config']
                       if 'Gateway' in v and ':' not in v['Gateway'])
        self.inner('run', '--rm', '--user', '65534:65534', '--cap-drop', 'ALL', '--read-only', '--memory=48m', '--pids-limit=32',
                   '--entrypoint', 'python3', IMAGE, '-c', n.CLIENT, gateway, '24443', 'allow')

    def setup(self):
        self.inner('network', 'create', '--subnet=172.27.42.0/24', '--ipv6', '--subnet=fd70:42::/64',
                   '--opt', 'com.docker.network.bridge.name=br-review', 'reviewnet')
        self.app = self.node('ordinary-app', 'reviewnet', ('--ip=172.27.42.2', '--ip6=fd70:42::2', '-p', '24443:443'))
        self.echo(self.app); self.nat()
        self.probe = self.node('owned-probe'); self.edge = self.node('owned-edge')
        self.peer(self.probe, 'eth0', 'probe0'); self.peer(self.edge, 'eth0', 'wan0')
        for cid, ip in [(self.probe, '198.18.2.2'), (self.edge, '198.18.3.2')]:
            self.inside(cid, 'ip', 'addr', 'add', ip+'/30', 'dev', 'eth0')
        for dev, ip in [('probe0', '198.18.2.1'), ('wan0', '198.18.3.1')]:
            self.root('ip', 'addr', 'add', ip+'/30', 'dev', dev)
        for ip in ['93.184.216.34', '93.184.216.35', '1.1.1.1']:
            self.inside(self.edge, 'ip', 'addr', 'add', ip+'/32', 'dev', 'lo')
            self.root('ip', 'route', 'add', ip+'/32', 'via', '198.18.3.2')
            self.inside(self.probe, 'ip', 'route', 'add', ip+'/32', 'via', '198.18.2.1')
        # Exercise Probe SNAT as well as unrelated published-port Docker NAT.
        self.root('nft', '-f', '-', data=b'table ip dpl_wire_nat {\n chain post {\n type nat hook postrouting priority 100; policy accept;\n iifname "probe0" oifname "wan0" ip saddr 198.18.2.2 masquerade\n }\n}\n')
        for cid, dev, ip in [(self.probe, 'eth0', 'fd70:2::2'), (self.edge, 'eth0', 'fd70:3::2'), (None, 'probe0', 'fd70:2::1'), (None, 'wan0', 'fd70:3::1')]:
            (self.root if cid is None else lambda *a: self.inside(cid, *a))('ip', '-6', 'addr', 'add', ip+'/64', 'dev', dev, 'nodad')
        self.inside(self.edge, 'ip', '-6', 'addr', 'add', '2001:4860:ffff::34/128', 'dev', 'lo', 'nodad')
        self.inside(self.edge, 'ip', '-6', 'route', 'add', 'fd70:2::/64', 'via', 'fd70:3::1')
        self.inside(self.probe, 'ip', '-6', 'route', 'add', '2001:4860:ffff::34/128', 'via', 'fd70:2::1')
        self.root('ip', '-6', 'route', 'add', '2001:4860:ffff::34/128', 'via', 'fd70:3::2')
        for cid, dev, peer, remote, address in [(None, 'probe0', self.probe, 'eth0', 'fd70:2::2'), (None, 'wan0', self.edge, 'eth0', 'fd70:3::2'), (self.probe, 'eth0', None, 'probe0', 'fd70:2::1'), (self.edge, 'eth0', None, 'wan0', 'fd70:3::1')]:
            mac = json.loads((self.root if peer is None else lambda *a: self.inside(peer, *a))('ip', '-j', 'link', 'show', 'dev', remote))[0]['address']
            (self.root if cid is None else lambda *a: self.inside(cid, *a))('ip', '-6', 'neigh', 'replace', address, 'lladdr', mac, 'nud', 'permanent', 'dev', dev)
        self.echo(self.edge)
        # Adversarial IPv6 routing is enabled only in this owned test daemon;
        # prove the path works before requiring the Lite guard to deny it.
        self.root('ip6tables', '-P', 'FORWARD', 'ACCEPT')
        self.attempt(self.probe, '93.184.216.34', 443, 'allow')
        self.attempt(self.probe, '2001:4860:ffff::34', 443, 'allow')
        pending = HostFilter(None, 'coexist', 'probe0', '198.18.2.2', '1.1.1.1', '93.184.216.34', 'wan0')
        ns = self.root('readlink', '/proc/self/ns/net').decode().strip()
        self.executor = NamespaceExecutor(json.loads(outer('inspect', self.cid).stdout)[0]['State']['Pid'], ns, ['probe0', 'wan0'], pending.tables)
        self.filter = HostFilter(self.executor.run, 'coexist', 'probe0', '198.18.2.2', '1.1.1.1', '93.184.216.34', 'wan0')
        self.filter.install(); self.filter.grant(deadline(), True)
        self.attempt(self.probe, '93.184.216.34', 443, 'allow'); self.attempt(self.probe, '1.1.1.1', 53, 'allow')
        self.attempt(self.probe, '93.184.216.35', 443, 'deny'); self.attempt(self.probe, '2001:4860:ffff::34', 443, 'deny')
        self.nat(); self.record('coexist', authorized_snat=True, ipv6_unauthorized_denied=True, unrelated_docker_nat_unchanged=True)

    def actor_state(self, pid):
        return json.loads(self.root('python3', '-c', 'from pathlib import Path;import sys;print((Path("/proc")/sys.argv[1]/"root/tmp/state.json").read_text())', str(pid)))

    def exercise(self):
        self.filter.grant(deadline(), True)
        code = '''import json,os,socket,time
from pathlib import Path
rows=[];p=Path('/tmp/state.json');s=socket.create_connection(('93.184.216.34',443),2);s.settimeout(.2);end=time.monotonic()+40
while time.monotonic()<end:
 start=time.monotonic()
 try:s.sendall(b'x');ok=s.recv(1)==b'x'
 except OSError:ok=False
 row=dict(start=start,allowed=ok)
 if Path('/tmp/new-request').exists():
  try:t=socket.create_connection(('93.184.216.34',443),.2);t.sendall(b'x');row['new']=t.recv(1)==b'x';t.close()
  except OSError:row['new']=False
 rows.append(row);q=p.with_suffix('.tmp');q.write_text(json.dumps(rows));os.replace(q,p);time.sleep(.05)
'''
        actor = self.inner('run', '-d', '--name', 'owned-actor', '--network', 'container:'+self.probe, '--user', '65534:65534', '--cap-drop', 'ALL',
                           '--read-only', '--memory=48m', '--pids-limit=32', '--tmpfs', '/tmp:rw,size=4m', '--entrypoint', 'python3', IMAGE, '-c', code).decode().strip()
        pid = json.loads(self.inner('inspect', actor))[0]['State']['Pid']
        end = time.monotonic()+5
        while time.monotonic() < end:
            try:
                if self.actor_state(pid)[-1]['allowed']:
                    break
            except RuntimeError:
                pass
            time.sleep(.05)
        else:
            raise RuntimeError('bounded actor readiness')
        self.stop_daemon()
        unavailable = outer('exec', self.cid, 'docker', 'info', check=False, timeout=2).returncode != 0
        assert unavailable
        self.filter.close(); closed_at = time.monotonic()
        if self.live:
            self.root('python3', '-c', 'from pathlib import Path;import sys;(Path("/proc")/sys.argv[1]/"root/tmp/new-request").touch()', str(pid))
            end = time.monotonic()+3
            while time.monotonic() < end:
                rows = [r for r in self.actor_state(pid) if r['start'] > closed_at and 'new' in r]
                if len(rows) >= 2:
                    break
                time.sleep(.05)
            assert len(rows) >= 2 and all(not r['allowed'] and not r['new'] for r in rows)
            self.record('daemon_down_revoke', api_unavailable=True, existing_and_new_denied=True, docker_independent=True)
        self.start_daemon()
        if self.live:
            self.attempt(self.probe, '93.184.216.34', 443, 'deny'); self.nat()
            self.record('live_restore_restart', admission_stayed_closed=True, unrelated_nat_unchanged=True)
        else:
            values = json.loads(self.inner('inspect', self.probe, self.edge, actor))
            assert not any(v['State']['Running'] for v in values)
            self.record('normal_restart', actors_stopped=True, admission_stayed_closed=True, receipts_inferred=False)
        self.record('pass', passed=True)

    def close(self):
        errors = []
        if self.executor:
            try: self.executor.close()
            except Exception as e: errors.append(type(e).__name__)
        if self.cid:
            if outer('rm', '-fv', self.cid, check=False).returncode:
                errors.append('owned_container_cleanup')
        # Directory removal only, strictly this new empty cgroup, no control-file
        # deletion, global controller mutation or other container operations.
        script = 'from pathlib import Path;import os,re,sys;name=sys.argv[1];assert re.fullmatch("dpl-lite-[0-9a-f]{16}",name);p=Path("/sys/fs/cgroup")/name;\nif not p.exists():raise SystemExit(0)\nds=sorted([x for x in p.rglob("*") if x.is_dir()],key=lambda x:len(x.parts),reverse=True);[os.rmdir(x) for x in ds];os.rmdir(p)'
        cleanup = outer('run', '--rm', '--network', 'none', '--privileged', '--cgroupns', 'host', '--memory=64m', '--pids-limit=32',
                        '--entrypoint', 'python3', IMAGE, '-c', script, self.cg, check=False)
        if cleanup.returncode:
            errors.append('owned_cgroup_cleanup')
        self.record('cleanup', errors=errors)
        if errors:
            raise RuntimeError('test cleanup not verified')


def main():
    dest = Path(sys.argv[1]).resolve(); dest.mkdir(exist_ok=False)
    for live in (True, False):
        rig = Rig(dest/('live-restore' if live else 'normal'), live)
        try:
            rig.boot(); rig.setup(); rig.exercise()
        finally:
            rig.close()
    print('PASS isolated Docker NAT, IPv6 and daemon-independent revoke', flush=True)


if __name__ == '__main__':
    main()
