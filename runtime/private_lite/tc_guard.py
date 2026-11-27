"""Independent per-packet guard on two fresh veths, outside nft rule lifetime."""
import hashlib
import json
import re

ENTRY = '0x4c300000'
EXIT = '0x4c310002'


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate TC JSON key')
        result[key] = value
    return result


def filter_json(raw):
    """Decode JSON, plus only the pre-6.8 fw printer's known bare handle.

    iproute2 v5.15 tc/f_fw.c uses fprintf even under -j. Match the complete
    guard-record header, retain both mark and mask, then parse strictly again.
    Never discard stdout, classifiers or unknown malformed output.
    """
    text = raw.decode('utf-8') if isinstance(raw, bytes) else raw
    def reject_constant(value):
        raise ValueError('non-finite TC JSON value')
    def decode(value):
        return json.loads(value, object_pairs_hook=unique_object, parse_constant=reject_constant)
    try:
        return decode(text)
    except json.JSONDecodeError:
        pattern = (r'(\{"protocol":"ip","pref":10,"kind":"fw","chain":0,"options":\{)'
                   r'handle (0x[0-9a-f]{1,8})(?:/(0x[0-9a-f]{1,8}))?\s+(?="actions":)')
        def replace(match):
            fw = dict(mark=hex(int(match[2], 16)), mask=hex(int(match[3], 16)) if match[3] else '0xffffffff')
            return match[1] + '"fw":' + json.dumps(fw, separators=(',', ':')) + ','
        matches = list(re.finditer(pattern, text))
        if len(matches) != 1:
            raise ValueError('unsupported malformed TC JSON')
        # A dummy final record must close the root array here. A match inside
        # another record/string/array cannot satisfy that strict JSON prefix.
        if not isinstance(decode(text[:matches[0].start()] + '{}]'), list):
            raise ValueError('legacy fw must be a top-level TC record')
        fixed, count = re.subn(pattern, replace, text)
        if count != 1:
            raise ValueError('unsupported malformed TC JSON')
        return decode(fixed)


def verified_filters(raw, direction, closed):
    """Require the installed guard, not a hash of an empty/incomplete dump."""
    if direction not in ('ingress', 'egress'):
        raise ValueError('unknown TC direction')
    expected = {(100, 'all', 'matchall'): [('skbedit', int(ENTRY, 16)), ('gact', 'pass')]} if direction == 'ingress' else {
        (5, 'arp', 'matchall'): [('gact', 'pass')],
        (10, 'ip', 'fw'): [('skbedit', 0), ('gact', 'pass')],
        (1000, 'all', 'matchall'): [('gact', 'drop')],
    }
    if closed and direction == 'egress':
        expected[(1, 'all', 'matchall')] = [('gact', 'drop')]
    rows, full, headers = filter_json(raw), set(), set()
    if not isinstance(rows, list):
        raise ValueError('TC filter array required')
    for row in rows:
        if (not isinstance(row, dict) or type(row.get('pref')) is not int
                or type(row.get('chain')) is not int or row['chain'] != 0):
            raise ValueError('TC filter identity')
        key = (row['pref'], row.get('protocol'), row.get('kind'))
        if key not in expected:
            raise ValueError('unexpected TC priority/protocol/classifier')
        if 'options' not in row:
            if key in headers:
                raise ValueError('duplicate TC header')
            headers.add(key)
            continue
        options = row['options']
        if key in full or not isinstance(options, dict) or options.get('skip_sw', False):
            raise ValueError('TC filter options')
        full.add(key)
        if key[2] == 'fw' and options.get('fw') != dict(mark=EXIT, mask='0xffffffff'):
            raise ValueError('TC full fw mark/mask required')
        actions, signatures = options.get('actions'), []
        if not isinstance(actions, list) or not actions:
            raise ValueError('TC actions missing')
        for order, action in enumerate(actions, 1):
            if (not isinstance(action, dict) or type(action.get('order')) is not int
                    or action['order'] != order):
                raise ValueError('TC action order')
            kind, control = action.get('kind'), action.get('control_action')
            no_random = dict(random_type='none', control_action=dict(type='pass'), val=0)
            if (kind == 'skbedit' and control == dict(type='pipe')
                    and type(action.get('mark')) is int
                    and action.get('mask', 0xffffffff) == 0xffffffff):
                signatures.append((kind, action['mark']))
            elif (kind == 'gact' and control in (dict(type='pass'), dict(type='drop'))
                    and action.get('prob', no_random) == no_random):
                signatures.append((kind, control['type']))
            else:
                raise ValueError('unproven TC action')
        if signatures != expected[key]:
            raise ValueError('TC action/mark mismatch')
    if full != set(expected):
        raise ValueError('TC guard rules missing')
    return rows


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
            queues = json.loads(self.run('tc', '-j', 'qdisc', 'show', 'dev', iface))
            if not isinstance(queues, list) or sum(q.get('kind') == 'clsact' for q in queues) != 1:
                raise ValueError('TC clsact missing')
            values.append(queues)
            for direction in ('ingress', 'egress'):
                raw = self.run('tc', '-j', 'filter', 'show', 'dev', iface, direction)
                values.append(verified_filters(raw, direction, self.closed))
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
        if self.closed:
            # matchall supports one classifier per priority. Revalidate the
            # existing deny anchor; do not attempt a second implicit insertion.
            self.check()
            return
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
