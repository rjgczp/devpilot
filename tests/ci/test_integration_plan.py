import copy
import json
from pathlib import Path
import unittest

from run_integration import check_plan, check_results, listed_tests


class IntegrationCoverageContract(unittest.TestCase):
    def setUp(self):
        self.groups = json.loads(Path(__file__).with_name('integration-groups.json').read_text())['groups']
        self.names = [name for group in self.groups for name in group['tests']]

    def test_every_discovered_test_is_assigned_once(self):
        self.assertEqual(check_plan(self.groups, self.names), self.names)
        with self.assertRaises(ValueError):
            check_plan(self.groups, self.names + ['TestNewUnassignedSecurityCase'])
        with self.assertRaises(ValueError):
            check_plan(self.groups, self.names[1:])
        groups = copy.deepcopy(self.groups)
        groups[0]['tests'].append(self.names[0])
        with self.assertRaises(ValueError):
            check_plan(groups, self.names)

    def test_real_deadlines_cannot_rejoin_cumulative_process(self):
        groups = copy.deepcopy(self.groups)
        groups[-1]['tests'].append(groups[0]['tests'].pop())
        with self.assertRaises(ValueError):
            check_plan(groups, self.names)

    def test_module_download_notices_preserve_complete_inventory(self):
        notices = '\n'.join([
            'go: downloading github.com/jackc/pgx/v5 v5.8.0',
            'go: downloading golang.org/x/net v0.49.0',
            'go: downloading golang.org/x/sys v0.41.0',
            'go: downloading golang.org/x/crypto v0.48.0',
            'go: downloading golang.org/x/text v0.34.0',
            'go: downloading golang.org/x/sync v0.19.0',
        ])
        events = [json.dumps({'Action': 'output', 'Package': 'pkg', 'Output': name + '\n'})
                  for name in self.names]
        found = listed_tests(notices + '\n' + '\n'.join(events))
        self.assertEqual(found, {'pkg': self.names})
        self.assertEqual(check_plan(self.groups, found['pkg']), self.names)
        missing = listed_tests(notices + '\n' + '\n'.join(events[1:]))
        with self.assertRaises(ValueError):
            check_plan(self.groups, missing['pkg'])

    def test_inventory_rejects_unknown_output_and_corrupt_events(self):
        for line in [
            '{"Action":"output","Package":"pkg","Output":"TestRequired\\n"',
            '{"Action":"fail",',
            'go: downloading github.com/jackc/pgx/v5 v5.8.0 {"Action":"fail"}',
            'go: downloading github.com/jackc/pgx/v5',
            'go: downloading github.com/jackc/pgx/v5 v5.8.0 trailing garbage',
            'go: warning: unexpected diagnostic',
            '',
        ]:
            with self.subTest(line=line), self.assertRaises(json.JSONDecodeError):
                listed_tests(line + '\n')
        with self.assertRaises(KeyError):
            listed_tests('{"Package":"pkg","Output":"TestRequired\\n"}')

    def test_skip_or_zero_test_success_cannot_pass(self):
        expected = {'pkg': ['TestRequired']}
        def events(*actions):
            return '\n'.join(json.dumps(row) for row in actions)
        run = {'Action': 'run', 'Package': 'pkg', 'Test': 'TestRequired'}
        passed = {'Action': 'pass', 'Package': 'pkg', 'Test': 'TestRequired'}
        package = {'Action': 'pass', 'Package': 'pkg'}
        self.assertEqual(check_results(events(run, passed, package), expected), 1)
        for log in [events(package), events(run, {'Action': 'skip', 'Package': 'pkg', 'Test': 'TestRequired'}, package), events(run, passed, run, passed, package)]:
            with self.assertRaises(ValueError):
                check_results(log, expected)
        with self.assertRaises(ValueError):
            check_results(events(run, passed, {'Action': 'skip', 'Package': 'pkg', 'Test': 'TestRequired/subcase'}, package), expected)
        for log in [
            events(run, {'Action': 'fail', 'Package': 'pkg', 'Test': 'TestRequired'}, package),
            events(run, passed, {'Action': 'fail', 'Package': 'pkg'}),
        ]:
            with self.assertRaises(ValueError):
                check_results(log, expected)
        with self.assertRaises(json.JSONDecodeError):
            check_results(events(run, passed, package) + '\n{"Action":"pass",', expected)


if __name__ == '__main__':
    unittest.main()
