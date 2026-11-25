#!/usr/bin/env python3
"""Explicit, foreground Private-Lite prototype. No default mode or deployment."""
import argparse
import json
import os
from pathlib import Path
import re
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent / 'private'))
import launcher as legacy
from kernel import Kernel, require, utc
from egress import HostFilter, public4


def load(path):
    p = Path(path)
    st = p.lstat()
    require(p.is_absolute() and p.is_file() and not p.is_symlink()
            and st.st_uid == os.getuid() and st.st_mode & 0o077 == 0 and st.st_size <= 32768)
    c = json.loads(p.read_text())
    require(c['mode'] == 'private-lite' and c['enabled'] is True and 'routed_uplink' not in c)
    lite = c['lite']
    require(set(lite) == {'resolver', 'host_container', 'host_interface', 'source'})
    lite['resolver'] = public4(lite['resolver'])
    target = public4(c['target_ip'])
    # Full existing config, approval, five identities and file checks. The SQL
    # protocol remains private-approved; only this runtime is named private-lite.
    checked = dict(c, mode='private-approved')
    legacy.validate_config(checked)
    require(target in c['approval']['addresses'] if 'approval' in c else target in checked['approval']['addresses'])
    c['approval'] = checked['approval']
    require(re.fullmatch('[0-9a-f]{64}', lite['host_container']))
    return c


class LiteKernel(Kernel):
    network_tool = '/toolbin/litenetwork'
    probe_tool = '/toolbin/liteprobe'
    policy_path = '/run/private-lite.json'

    def dns_routes(self):
        return [(self.cfg['lite']['resolver'], '-4', '/32')]

    def network_config(self, config):
        return dict(config, lite=dict(resolver=self.cfg['lite']['resolver']))

    def probe_policy(self):
        return dict(super().probe_policy(), resolver=self.cfg['lite']['resolver'])

    def validate_target(self):
        lite = self.cfg['lite']
        # This prototype accepts ONLY a disconnected test-host namespace. It is
        # intentionally not a runnable root-host/Lighthouse deployment command.
        for cid in (self.target, lite['host_container']):
            v = self.inspect(cid); h = v['HostConfig']
            require(v['State']['Running'] and v['Image'] == self.image and h['NetworkMode'] == 'none'
                    and not h['Privileged'] and not h['PortBindings'] and not h['PidMode']
                    and h['ReadonlyRootfs'] and h['CapDrop'] == ['ALL']
                    and h['CapAdd'] in (['NET_ADMIN'], ['CAP_NET_ADMIN'])
                    and not h['Binds'] and not h['Devices'])
            for family in ('-4', '-6'):
                require(not json.loads(self.exec(cid, 'ip', family, '-j', 'route', 'show', 'default')))
        host = lite['host_container']
        require(host != self.target and host != self.cfg['pg_container'])
        links=json.loads(self.exec(self.target,'ip','-j','link'))
        require({'lo','uplink0'} <= {v['ifname'] for v in links})
        templates={'tunl0','gre0','gretap0','erspan0','ip_vti0','ip6_vti0','sit0','ip6tnl0','ip6gre0'}
        for v in links:
            if v['ifname'] in {'lo','uplink0'}:continue
            require(v['ifname'] in templates and v['operstate']=='DOWN' and 'UP' not in v['flags'])
            require(all(not x.get('addr_info') for x in json.loads(self.exec(self.target,'ip','-j','addr','show','dev',v['ifname']))))
            for family in ('-4','-6'):
                require(not json.loads(self.exec(self.target,'ip',family,'-j','route','show','table','all','dev',v['ifname'])))
        interface = next(v for v in json.loads(self.exec(host, 'ip', '-j', 'link')) if v['ifname'] == lite['host_interface'])
        require(interface.get('link_index') == next(v for v in json.loads(self.exec(self.target, 'ip', '-j', 'link')) if v['ifname'] == 'uplink0')['ifindex'])
        self.host_filter = HostFilter(lambda *a, **kw: self.exec(host, *a, **kw), self.prefix,
                                      lite['host_interface'], lite['source'], lite['resolver'], self.cfg['target_ip'])
        self.host_filter.install()
        # Independent namespace default deny, no PCI requirement in Lite only.
        import uplink
        config = dict(mode='private-approved', target_ip=self.cfg['target_ip'], approval=self.approval,
                      routed_uplink=dict(pci_slot='0000:00:00.0', mac='00:00:00:00:00:00'))
        rules = uplink.rules(config).replace('192.0.2.53', lite['resolver'])
        self.exec(self.target, 'nft', '-f', '-', data=rules.encode())

    def prepare(self, index, line):
        # Reload the operator file for each allocation; no catalog mutation or
        # expired approval can be hidden behind a cached runtime configuration.
        from approval import load_approval, bound
        approval = load_approval(self.cfg['approval_file'], self.cfg['target_host'], self.cfg['target_ip'])
        require(approval == self.approval and bound(approval, line))
        self.host_filter.grant(utc(34))
        return super().prepare(index, line)

    def call(self, index, command):
        self.host_filter.check()
        result = super().call(index, command)
        if command['op'] == 'install':
            self.host_filter.grant(command['permit']['permit_deadline'], target=True)
        return result

    def _stop(self, index):
        self.host_filter.revoke()
        return super()._stop(index)


class LiteRuntime(legacy.Runtime):
    kernel_type = LiteKernel

    def worker_mode(self):
        return 'private-approved'


def main():
    p = argparse.ArgumentParser()
    p.add_argument('action', choices=['init', 'run', 'stop', 'status'])
    p.add_argument('--state', type=Path, required=True)
    p.add_argument('--config', type=Path)
    p.add_argument('--enable-private-lite', action='store_true')
    a = p.parse_args()
    require(sys.platform == 'linux' and not sys.flags.optimize)
    state = legacy.private_dir(a.state)
    if a.action == 'init':
        from assets.authority import Authority
        authority = Authority(state); authority.pub.chmod(0o444)
        return 0
    if a.action == 'status':
        print((state/'status.json').read_text() if (state/'status.json').exists() else '{"event":"closed"}')
        return 0
    if a.action == 'stop':
        return legacy.stop(state)
    if not a.enable_private_lite:
        print('{"event":"private_lite_closed","execution_available":false}')
        return 0
    return legacy.serve(load(a.config), state, LiteRuntime)


if __name__ == '__main__':
    raise SystemExit(main())
