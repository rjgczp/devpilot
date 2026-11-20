"""Exercise real child EOF/reaping with Docker effects stubbed out."""
from pathlib import Path
import subprocess
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'scheduler'))
from periodic91 import PeriodicHarness


class PeriodicDriverExitTest(unittest.TestCase):
    def run_driver(self, exit_code=0, hang=False, truncated=False, read_error=False):
        with tempfile.TemporaryDirectory() as folder:
            h = PeriodicHarness.__new__(PeriodicHarness)
            h.dest = Path(folder)
            h.prefix = 'unit-only'
            h.pg_id = 'no-docker'
            h.processes = []
            h.completed = []
            h.worker_log = []
            h.receiver_events = []
            h.expected_cycles = 0
            h.expected_gets = 0
            h.pg_deadline_seconds = .2
            h.setup_control = lambda: None
            records = {}
            h.record = lambda name, data: records.update({name: data})
            h.driver_log_line = lambda line: line
            output = "os.write(1,b'partial')" if truncated else "print('PASS',flush=True)"
            child_code = ("import os,sys;" + output +
                          ";os.close(1);os.close(2);sys.stdin.buffer.read(1);sys.exit(%d)" % exit_code)
            h.control_command = lambda *a, **k: [sys.executable, '-c', child_code]
            real_popen = subprocess.Popen
            children = []

            def spawn(*args, **kwargs):
                p = real_popen(*args, **kwargs)
                children.append(p)
                if not hang:
                    real_wait = p.wait
                    released = False

                    def wait(*a, **k):
                        nonlocal released
                        if not released:
                            released = True
                            p.stdin.write(b'x')
                            p.stdin.flush()
                        return real_wait(*a, **k)
                    p.wait = wait
                if read_error:
                    # A completed child must not cause a non-EOF read error to
                    # be swallowed by the outer loop, as the old catch-all did.
                    p.wait(timeout=5)
                    h.read_bytes = lambda *a: (_ for _ in ()).throw(RuntimeError('read deadline expired'))
                return p

            try:
                with patch('periodic91.subprocess.Popen', side_effect=spawn), \
                     patch('periodic91.subprocess.check_output', return_value=b''), \
                     patch('periodic91.subprocess.run', return_value=types.SimpleNamespace(stdout=b'', stderr=b'')):
                    if hang:
                        with self.assertRaises(subprocess.TimeoutExpired):
                            h.pg(Path(__file__), 'normal')
                    elif exit_code or truncated or read_error:
                        with self.assertRaises((AssertionError, RuntimeError)):
                            h.pg(Path(__file__), 'normal')
                    else:
                        h.pg(Path(__file__), 'normal')
                self.assertEqual('result' in records, not (hang or exit_code or truncated or read_error))
                if not truncated:
                    self.assertIsNotNone(records['pg-driver-command']['exit_code'])
                if hang:
                    self.assertLess(children[0].returncode, 0)
                elif not truncated:
                    self.assertEqual(children[0].returncode, exit_code)
            finally:
                for p in children:
                    if p.poll() is None:
                        p.kill()
                        p.wait(timeout=5)
                    p.stdin.close()
                    p.stdout.close()

    def test_eof_before_successful_exit_is_reaped(self):
        self.run_driver()

    def test_eof_before_abnormal_exit_is_rejected(self):
        self.run_driver(exit_code=7)

    def test_eof_without_exit_is_bounded_killed_and_reaped(self):
        self.run_driver(hang=True)

    def test_partial_frame_is_rejected(self):
        self.run_driver(truncated=True)

    def test_non_eof_failure_is_not_swallowed_after_exit(self):
        self.run_driver(read_error=True)


if __name__ == '__main__':
    unittest.main()
