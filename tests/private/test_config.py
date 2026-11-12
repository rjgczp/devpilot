import importlib.util
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('private_config', ROOT/'deploy/private/configure.py')
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PrivateConfigTest(unittest.TestCase):
    def render(self, **changes):
        values = dict(PRIVATE_BIND_IP='10.44.0.1', PRIVATE_HTTPS_PORT='443',
                      PRIVATE_HOST='devpilot.private.example', PRIVATE_ALLOWED_CIDRS='10.44.0.0/24')
        values.update(changes)
        with tempfile.TemporaryDirectory() as temp:
            file = Path(temp)/'env'
            file.write_text('\n'.join(k+'='+v for k, v in values.items()))
            return module.render(file)

    def test_private_boundary_and_canonical_origin(self):
        config = self.render()
        self.assertIn('allow 10.44.0.0/24;', config)
        self.assertIn('deny all;', config)
        self.assertIn('"https://devpilot.private.example"', config)
        self.assertNotIn('@@', config)
        self.assertIn('"https://devpilot.private.example:8443"', self.render(PRIVATE_HTTPS_PORT='8443'))

    def test_unsafe_settings_cannot_generate_a_listener(self):
        cases = [dict(PRIVATE_BIND_IP=s) for s in ('0.0.0.0', '8.8.8.8', '::', '169.254.169.254')]
        cases += [dict(PRIVATE_ALLOWED_CIDRS=s) for s in ('0.0.0.0/0', '::/0', '8.8.8.0/24', '10.0.0.1/24', '10.0.0.0/8;allow all')]
        cases += [dict(PRIVATE_HOST=s) for s in ('example.com;deny off', 'https://example.com', 'foo..com', '-foo.com', '127.0.0.1')]
        cases += [dict(PRIVATE_HTTPS_PORT=s) for s in ('0', '65536', '0443', '443\nINJECT=yes')]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(ValueError):
                self.render(**case)


if __name__ == '__main__':
    unittest.main()
