"""Independent per-packet guard on two fresh veths, outside nft rule lifetime."""
import hashlib
import json
import re

ENTRY = '0x4c300000'
EXIT = '0x4c310002'


def stable(value):
    if isinstance(value, dict):
        return {k: stable(v) for k, v in value.items() if k not in (
            'stats', 'used_hw_stats', 'ref', 'bind', 'lastuse', 'lastused', 'firstused', 'installed', 'used')}
    if isinstance(value, list):
        return [stable(v) for v in value]
    return value


class TCGuard:
    def __init__(self, run, interfaces):
        if len(interfaces) != 2 or len(set(interfaces)) != 2 or any(not re.fullmatch('[a-zA-Z0-9_-]{1,15}', v) for v in interfaces):
            raise ValueError('two owned veths required')
        self.run, self.interfaces, self.hash = run, interfaces, None
        self.closed = False

    def snapshot(self):
        values = []
        present = {v['ifname'] for v in json.loads(self.run('ip', '-j', 'link', 'show'))}
        for iface in self.interfaces:
            if self.closed and iface not in present:
                values.append(dict(missing=iface))
                continue
            values.append(json.loads(self.run('tc', '-j', 'qdisc', 'show', 'dev', iface)))
            for direction in ('ingress', 'egress'):
                values.append(json.loads(self.run('tc', '-j', 'filter', 'show', 'dev', iface, direction)))
        return hashlib.sha256(json.dumps(stable(values), sort_keys=True).encode()).hexdigest()

    def install(self):
        for iface in self.interfaces:
            queues = json.loads(self.run('tc', '-j', 'qdisc', 'show', 'dev', iface))
            if any(v['kind'] in ('clsact', 'ingress') for v in queues):
                raise ValueError('existing TC owner: stay closed')
            self.run('tc', 'qdisc', 'add', 'dev', iface, 'clsact')
            # Deny anchor is installed first, before any allow or actor.
            self.run('tc', 'filter', 'add', 'dev', iface, 'egress', 'pref', '1000', 'protocol', 'all', 'matchall', 'action', 'drop')
        for iface in self.interfaces:
            self.run('tc', 'filter', 'add', 'dev', iface, 'ingress', 'pref', '100', 'protocol', 'all', 'matchall', 'action', 'skbedit', 'mark', ENTRY, 'action', 'pass')
            self.run('tc', 'filter', 'add', 'dev', iface, 'egress', 'pref', '5', 'protocol', 'arp', 'matchall', 'action', 'pass')
            self.run('tc', 'filter', 'add', 'dev', iface, 'egress', 'pref', '10', 'protocol', 'ip', 'handle', EXIT, 'fw', 'action', 'skbedit', 'mark', '0', 'action', 'pass')
        self.hash = self.snapshot()

    def check(self):
        if not self.hash or self.snapshot() != self.hash:
            raise ValueError('TC guard drift')

    def close(self):
        self.closed = True
        present = {v['ifname'] for v in json.loads(self.run('ip', '-j', 'link', 'show'))}
        for iface in self.interfaces:
            if iface in present:
                self.run('tc', 'filter', 'replace', 'dev', iface, 'egress', 'pref', '1', 'protocol', 'all', 'matchall', 'action', 'drop')
        self.hash = self.snapshot()

    def retire(self):
        self.check()
        present = {v['ifname'] for v in json.loads(self.run('ip', '-j', 'link', 'show'))}
        for iface in self.interfaces:
            if iface in present:
                self.run('tc', 'qdisc', 'del', 'dev', iface, 'clsact')
        self.hash = None
