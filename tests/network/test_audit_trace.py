"""Policy regression corpus; actual Linux calibration remains mandatory."""
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from audit_trace import classify


class DestinationPolicy(unittest.TestCase):
    def test_original_six_nscd_attempts_still_fail(self):
        fixture = Path(__file__).parent / "fixtures/ci-39193625995-nscd.trace"
        approved, violations = classify(fixture.read_text().splitlines())
        self.assertEqual((approved, len(violations)), (0, 6))
        self.assertTrue(all(v == "unapproved Unix socket destination" for v in violations))
        result = subprocess.run([sys.executable, str(Path(__file__).with_name("audit_trace.py")),
                                 str(fixture)], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("blocked_attempts=6", result.stdout)

    def test_exact_tcp_fixtures_only(self):
        for port in (5432, 18081):
            line = f'4 connect(5<TCP:[1]>, {{sa_family=AF_INET, sin_port=htons({port}), sin_addr=inet_addr("127.0.0.1")}}, 16) = -1 EINPROGRESS'
            self.assertEqual(classify([line]), (1, []))
            self.assertEqual(len(classify([line.replace("TCP", "UDP")])[1]), 1)
            self.assertEqual(len(classify([line.replace("127.0.0.1", "127.0.0.2")])[1]), 1)

    def test_failed_control_is_rejected_by_cli(self):
        line = '4 connect(5<TCP:[1]>, {sa_family=AF_INET, sin_port=htons(80), sin_addr=inet_addr("192.0.2.1")}, 16) = -1 ECONNREFUSED\n'
        with tempfile.NamedTemporaryFile(mode="w", suffix=".trace") as trace:
            trace.write(line)
            trace.flush()
            result = subprocess.run([sys.executable, str(Path(__file__).with_name("audit_trace.py")),
                                     trace.name], capture_output=True, text=True)
        self.assertEqual(result.returncode, 1)
        self.assertIn("192.0.2.1:80", result.stdout)

    def test_unix_ipv6_dns_and_unknown_remain_closed(self):
        for line in (
            'connect(5<UNIX-STREAM:[1]>, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110) = 0',
            'connect(5, {sa_family=AF_UNIX, sun_path=@"runtime-ipc"}, 16) = -1 ENOENT',
            'sendmsg(5, {msg_name={sa_family=AF_LOCAL, sun_path="/tmp/other"}}, 0) = 1',
            'connect(5, {sa_family=AF_INET6, sin6_port=htons(5432), inet_pton(AF_INET6, "::1", &sin6_addr)}, 28) = 0',
            'sendto(5<UDP:[1]>, "dns", 3, 0, {sa_family=AF_INET, sin_port=htons(53), sin_addr=inet_addr("127.0.0.1")}, 16) = 3',
            'sendmmsg(5, [{msg_hdr={msg_name={sa_family=AF_UNIX, sun_path="/tmp/ipc"}}}], 1, 0) = 1',
            'connect(5, {sa_family=AF_VSOCK}, 16) = 0',
        ):
            with self.subTest(line=line):
                self.assertEqual(len(classify([line])[1]), 1)

    def test_process_provenance_does_not_exempt_destination(self):
        self.assertEqual(classify(['4 execve("/toolbin/goose", ["goose", "up"], 0x0) = 0']), (0, []))
        self.assertEqual(len(classify(['4 connect(5, {sa_family=AF_UNIX, sun_path="/var/run/nscd/socket"}, 110) = -1 ENOENT'])[1]), 1)


if __name__ == "__main__":
    unittest.main()
