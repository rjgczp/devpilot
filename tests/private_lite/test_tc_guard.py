import base64
import copy
import json
from pathlib import Path
import subprocess
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT/'runtime/private_lite'))
from tc_guard import TCGuard, filter_json, verified_filters

FIXTURES = Path(__file__).with_name('fixtures')
INGRESS = (FIXTURES/'tc-5.15-ingress.json').read_bytes()
LEGACY = base64.b64decode((FIXTURES/'tc-5.15-egress.stdout.b64').read_bytes().strip(), validate=True)
MODERN = (FIXTURES/'tc-modern-egress.json').read_bytes()


class KernelDump:
    """Command success never substitutes for proving the returned rules."""
    def __init__(self, legacy=True):
        self.legacy = legacy
        self.queues = set()
        self.closed = set()
        self.mutate = lambda rows: rows
        self.raw = None
        self.fail_read = False

    def run(self, *args):
        if args == ('ip', '-j', 'link', 'show'):
            return b'[{"ifname":"lite0"},{"ifname":"wan0"}]'
        if args[:3] == ('tc', 'qdisc', 'add'):
            self.queues.add(args[4])
        if args[:3] == ('tc', 'filter', 'replace'):
            self.closed.add(args[4])
        if args[:4] == ('tc', '-j', 'qdisc', 'show'):
            return json.dumps([dict(kind='clsact')] if args[5] in self.queues else []).encode()
        if args[:4] == ('tc', '-j', 'filter', 'show'):
            if self.fail_read:
                raise subprocess.CalledProcessError(2, args, b'[]', b'netlink failure')
            if self.raw is not None:
                return self.raw
            if args[-1] == 'ingress':
                return INGRESS
            if self.mutate is identity and args[5] not in self.closed:
                return LEGACY if self.legacy else MODERN
            rows = json.loads(MODERN)
            if args[5] in self.closed:
                rule = copy.deepcopy(next(v for v in rows if v['pref'] == 1000 and 'options' in v))
                rule['pref'] = 1
                rows.append(rule)
            return json.dumps(self.mutate(rows)).encode()
        return b''


def identity(rows):
    return rows


class TCCompatibility(unittest.TestCase):
    def test_exact_legacy_failure_and_lossless_modern_equivalence(self):
        with self.assertRaises(json.JSONDecodeError) as error:
            json.loads(LEGACY)
        self.assertEqual(error.exception.colno, 433)
        self.assertEqual(filter_json(LEGACY), json.loads(MODERN))
        self.assertEqual(verified_filters(LEGACY, 'egress', False), json.loads(MODERN))

    def test_modern_and_legacy_install_check_close_are_accepted(self):
        for legacy in (True, False):
            with self.subTest(legacy=legacy):
                kernel = KernelDump(legacy)
                kernel.mutate = identity
                guard = TCGuard(kernel.run, ['lite0', 'wan0'])
                guard.install()
                guard.check()
                guard.close()
                guard.close()
                guard.check()

    def test_unknown_corruption_and_stdout_mixing_fail_closed(self):
        for raw in [b'', b'warning\n'+LEGACY, LEGACY+b'warning', LEGACY[:-2],
                    LEGACY.replace(b'handle ', b'classid ', 1),
                    LEGACY.replace(b'0x4c310002', b'0x4c310002junk', 1),
                    b'[{"pref":NaN}]', b'[{"kind":"fw","kind":"matchall"}]', b'\xff']:
            with self.subTest(raw=raw[:45]):
                with self.assertRaises(ValueError):
                    verified_filters(raw, 'egress', False)

    def test_wrong_mark_and_partial_masks_are_never_discarded(self):
        for raw in [LEGACY.replace(b'handle 0x4c310002', b'handle 0x4c310001'),
                    LEGACY.replace(b'handle 0x4c310002', b'handle 0x4c310002/0xffff'),
                    MODERN.replace(b'0x4c310002', b'0x4c310001'),
                    MODERN.replace(b'0xffffffff', b'0xffff')]:
            with self.subTest(raw=raw[400:470]):
                with self.assertRaises(ValueError):
                    verified_filters(raw, 'egress', False)

    def test_legacy_repair_inside_foreign_nested_field_is_rejected(self):
        rows = json.loads(MODERN)
        fw = next(row for row in rows if row['kind'] == 'fw' and 'options' in row)
        nested = json.dumps(fw, separators=(',', ':')).replace(
            '"fw":{"mark":"0x4c310002","mask":"0xffffffff"},',
            'handle 0x4c310002 \n')
        for container in [dict(foreign='MARKER'), dict(foreign=['MARKER'])]:
            with self.subTest(container=container):
                rows[1].update(container)
                raw = json.dumps(rows, separators=(',', ':')).replace('"MARKER"', nested)
                with self.assertRaises(ValueError):
                    verified_filters(raw, 'egress', False)

    def test_empty_incomplete_and_wrong_priority_cannot_be_baseline(self):
        for mutation in [lambda rows: [],
                         lambda rows: [v for v in rows if v['pref'] != 1000],
                         lambda rows: [dict(v, pref=999) if v['pref'] == 1000 else v for v in rows],
                         lambda rows: [v for v in rows if 'options' not in v]]:
            kernel = KernelDump()
            kernel.mutate = mutation
            guard = TCGuard(kernel.run, ['lite0', 'wan0'])
            with self.assertRaises(ValueError):
                guard.install()
            self.assertIsNone(guard.hash)

    def test_rule_loss_or_changed_priority_after_install_rejects_check(self):
        for mutation in [lambda rows: [],
                         lambda rows: [dict(v, pref=999) if v['pref'] == 1000 else v for v in rows]]:
            kernel = KernelDump()
            kernel.mutate = identity
            guard = TCGuard(kernel.run, ['lite0', 'wan0'])
            guard.install()
            kernel.mutate = mutation
            with self.assertRaises(ValueError):
                guard.check()

    def test_changed_action_or_missing_action_rejects_proof(self):
        for change in ['allow-drop-anchor', 'empty-actions', 'partial-skbedit', 'probabilistic-drop']:
            rows = json.loads(MODERN)
            rule = next(v for v in rows if v['pref'] == 1000 and 'options' in v)
            if change == 'allow-drop-anchor':
                rule['options']['actions'][0]['control_action']['type'] = 'pass'
            elif change == 'empty-actions':
                rule['options']['actions'] = []
            elif change == 'probabilistic-drop':
                rule['options']['actions'][0]['prob']['random_type'] = 'netrand'
            else:
                next(v for v in rows if v['pref'] == 10 and 'options' in v)['options']['actions'][0]['mask'] = 0xff
            with self.subTest(change=change):
                with self.assertRaises(ValueError):
                    verified_filters(json.dumps(rows), 'egress', False)

    def test_nonzero_command_exit_never_becomes_success(self):
        kernel = KernelDump()
        kernel.fail_read = True
        guard = TCGuard(kernel.run, ['lite0', 'wan0'])
        with self.assertRaises(subprocess.CalledProcessError):
            guard.install()
        self.assertIsNone(guard.hash)

    def test_missing_clsact_or_closed_deny_anchor_rejects_check(self):
        kernel = KernelDump()
        kernel.mutate = identity
        guard = TCGuard(kernel.run, ['lite0', 'wan0'])
        guard.install()
        kernel.queues.clear()
        with self.assertRaises(ValueError):
            guard.check()
        with self.assertRaises(ValueError):
            verified_filters(MODERN, 'egress', True)


if __name__ == '__main__':
    unittest.main()
