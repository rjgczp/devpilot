"""Exercise the build gate with real Go binaries, including mixed CGO tools."""
import hashlib
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from check_audit_build import check


class AuditBuildContract(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        directory = Path(cls.temp.name)
        source = directory / "lookup.go"
        source.write_text('package main\nimport "os/user"\nfunc main() { _, _ = user.Current() }\n')
        host = subprocess.check_output(["go", "env", "GOHOSTOS"], text=True).strip()
        cls.pure = directory / "pure"
        cls.cgo = directory / "cgo"
        for binary, cgo, target in ((cls.pure, "0", "linux"), (cls.cgo, "1", host)):
            env = dict(os.environ, CGO_ENABLED=cgo, GOOS=target, GOTOOLCHAIN="local")
            subprocess.run(["go", "build", "-o", str(binary), str(source)], env=env,
                           check=True, capture_output=True, text=True)

    def test_real_linux_pure_build_has_bound_hash(self):
        record = check(str(self.pure))
        self.assertEqual(record["sha256"], hashlib.sha256(self.pure.read_bytes()).hexdigest())
        self.assertIn("CGO_ENABLED=0", record["metadata"])
        self.assertIn("GOOS=linux", record["metadata"])

    def test_cgo_in_either_tool_position_fails(self):
        gate = Path(__file__).with_name("check_audit_build.py")
        for binaries in ((self.cgo, self.pure), (self.pure, self.cgo)):
            with self.subTest(binaries=binaries):
                result = subprocess.run([sys.executable, str(gate), *map(str, binaries)],
                                        capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("requires CGO_ENABLED=0", result.stderr)
                self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
