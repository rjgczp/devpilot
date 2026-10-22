import copy
import json
from pathlib import Path
import unittest

from run_integration import check_plan, check_results


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


if __name__ == '__main__':
    unittest.main()
