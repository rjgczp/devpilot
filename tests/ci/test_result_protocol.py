"""Keep current refusal coverage separate from unchanged legacy assertions."""
import hashlib
from pathlib import Path
import runpy
import unittest

ROOT = Path(__file__).resolve().parents[2]
SCHEDULER = ROOT/'tests/scheduler'


class ResultProtocolContract(unittest.TestCase):
    def test_current_matrices_include_all_control_phases_and_legacy_refusal(self):
        controls = runpy.run_path(str(SCHEDULER/'run-result71.py'))['PLAN']
        current = runpy.run_path(str(SCHEDULER/'run-result72.py'))['PLAN']
        self.assertEqual(controls, [('TestResultMinimalPG', 'permit-missing'),
                                   ('TestResultMinimalPG', 'supervisor-killed'),
                                   ('TestResultPreflightPG', 'preflight-only')])
        self.assertEqual(len(current), 9)
        self.assertEqual(len(set(current)), 9)
        self.assertIn(('TestResultLegacyNetworkDisabledPG', 'allow-https'), current)

    def test_original_71_assertions_unchanged_and_history_pin_explicit(self):
        original = ROOT/'tests/integration/scheduler_minimal_result_test.go'
        self.assertEqual(hashlib.sha256(original.read_bytes()).hexdigest(),
                         '1ad62b367f57f82d93b565be732558a84abaa9408449a2594195a2ba1bba30a0')
        legacy = runpy.run_path(str(SCHEDULER/'run-legacy-result71.py'))
        self.assertEqual(legacy['LEGACY'], 'c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c')


if __name__ == '__main__':
    unittest.main()
