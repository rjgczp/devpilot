import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release_preflight', ROOT / 'deploy/release/preflight.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)


class ReleasePreflight(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name).resolve()
        self.base.chmod(0o700)
        self.secrets = self.base / 'secrets'
        self.secrets.mkdir(mode=0o700)
        for name in ('migration_password', 'app_password', 'app_url', 'tls.key', 'tls.crt', 'w0', 'w1', 'supervisor', 'controller', 'reporter', 'catalog', 'bindings', 'ca'):
            file = self.secrets / name
            file.write_text('DO_NOT_READ_SECRET_SENTINEL\n')
            file.chmod(0o444)
        self.state = self.base / 'state'
        self.state.mkdir(mode=0o700)
        (self.state / 'private.pem').write_text('DO_NOT_READ_PRIVATE_KEY_SENTINEL')
        (self.state / 'private.pem').chmod(0o600)
        (self.state / 'public.pem').write_text('public test material')
        (self.state / 'public.pem').chmod(0o444)
        self.env = self.base / 'private.env'
        self.env.write_text('PRIVATE_BIND_IP=10.44.0.1\nPRIVATE_HTTPS_PORT=443\nPRIVATE_HOST=devpilot.private.example\nPRIVATE_ALLOWED_CIDRS=10.44.0.0/24\n')
        self.runtime = json.loads((ROOT / 'deploy/privateservice/service.example.json').read_text())
        self.runtime.update(image='sha256:' + 'a' * 64, target_container='b' * 64, pg_container='c' * 64,
                            control_network='devpilot-private-control', catalog_file=str(self.secrets / 'catalog'),
                            bindings_file=str(self.secrets / 'bindings'), ca_file=str(self.secrets / 'ca'))
        for n, item in enumerate(self.runtime['workers']):
            item.update(database_url_file=str(self.secrets / ('w' + str(n))), incarnation=f'00000000-0000-4000-8000-00000000000{n+1}')
        for name in ('supervisor', 'controller', 'reporter'):
            self.runtime[name]['database_url_file'] = str(self.secrets / name)
        self.runtime_file = self.base / 'service.json'
        self.save_runtime()
        self.manifest = self.base / 'release.json'
        self.manifest.write_text(json.dumps(dict(desired_gate='closed', management_env=str(self.env),
            management_secrets=str(self.secrets), runtime_config=str(self.runtime_file),
            state_directory=str(self.state), vpn_interface='wg0')))
        self.commands = []

    def save_runtime(self):
        self.runtime_file.write_text(json.dumps(self.runtime))
        self.runtime_file.chmod(0o600)

    def offline_runner(self, args):
        self.commands.append(args)
        self.assertEqual(args[:2], ['openssl', 'x509'])
        return b'public certificate check mocked; not host acceptance'

    def inspect(self, **kw):
        return p.inspect(self.manifest, runner=self.offline_runner, **kw)

    def test_offline_plan_never_reads_secrets_or_claims_private_https_ready(self):
        before = {f: f.read_bytes() for f in self.base.rglob('*') if f.is_file()}
        original = Path.read_text
        def guarded(path, *args, **kwargs):
            self.assertNotIn(path, set(self.secrets.iterdir()) | {self.state / 'private.pem'})
            return original(path, *args, **kwargs)
        with mock.patch.object(Path, 'read_text', guarded):
            report = self.inspect()
        self.assertTrue(report['configuration_structure_valid'])
        self.assertFalse(report['private_https_release_ready'])
        self.assertFalse(report['real_https_execution_supported'])
        self.assertFalse(report['sql_identity_and_registry_verified'])
        self.assertEqual(len(report['blockers']), 3)
        self.assertNotIn('SENTINEL', json.dumps(report))
        self.assertEqual(before, {f: f.read_bytes() for f in self.base.rglob('*') if f.is_file()})

    def test_unsigned_run_or_recovery_required_stays_closed_even_with_local_flags(self):
        run_id = '00000000-0000-4000-8000-000000000007'
        (self.state / run_id).write_text('burned')
        owner = self.state / (run_id + '.owner.json')
        owner.write_text(json.dumps(dict(index=0, line=dict(run_id=run_id))))
        (self.state / (run_id + '.recovered')).write_text('not a physical release')
        report = self.inspect()
        self.assertTrue(report['journal']['recovery_required'])
        self.assertFalse(report['configuration_structure_valid'])
        for ext in ('.receipt', '.proofs'):
            (self.state / (run_id + ext)).write_text('NOT verified signed material')
        (self.state / 'status.json').write_text('{"event":"recovery_required"}')
        self.assertTrue(self.inspect()['journal']['recovery_required'])
        (self.state / 'status.json').unlink()
        report = self.inspect()
        self.assertFalse(report['journal']['signatures_verified'])
        self.assertFalse(report['journal']['recovery_clearance_verified'])
        self.assertFalse(report['private_https_release_ready'])

    def test_missing_or_modified_migration_and_candidate_are_rejected(self):
        copy = self.base / 'source'
        for name in ('migrations', 'deploy/release', 'deploy/privateloop', 'deploy/capacity'):
            shutil.copytree(ROOT / name, copy / name)
        self.assertEqual([r['version'] for r in p.schema(copy)], p.VERSIONS + [14, 15])
        rogue = copy / 'migrations/00011_forbidden.sql'
        rogue.write_text('-- not permitted')
        with self.assertRaises(ValueError): p.schema(copy)
        rogue.unlink()
        candidate = copy / 'deploy/capacity/continuous-capacity.sql.candidate'
        candidate.write_text(candidate.read_text() + '\n-- modified\n')
        with self.assertRaises(ValueError): p.schema(copy)

    def test_arbitrary_target_open_gate_and_unsafe_secret_are_not_configuration_shortcuts(self):
        self.runtime['target_host'] = 'real.example.com'
        self.save_runtime()
        self.assertFalse(self.inspect()['configuration_structure_valid'])
        self.runtime['target_host'] = 'a.fixture.example.com'
        self.save_runtime()
        self.secrets.chmod(0o755)
        self.assertFalse(self.inspect()['configuration_structure_valid'])
        self.secrets.chmod(0o700)
        value = json.loads(self.manifest.read_text())
        value['desired_gate'] = 'private-real'
        self.manifest.write_text(json.dumps(value))
        self.assertFalse(self.inspect().get('configuration_structure_valid', False))

    def test_errors_never_emit_daemon_or_secret_payload(self):
        def failing(args):
            raise subprocess.CalledProcessError(1, args, output=b'SECRET_SENTINEL', stderr=b'PRIVATE_KEY_SENTINEL')
        report = p.inspect(self.manifest, runner=failing)
        self.assertFalse(report['configuration_structure_valid'])
        self.assertNotIn('SENTINEL', json.dumps(report))

    def test_cli_requires_explicit_config_only_for_zero_exit_and_has_no_apply_verb(self):
        def invoke(*extra):
            with mock.patch.object(sys, 'argv', ['preflight', '--manifest', str(self.manifest), *extra]), mock.patch.object(p, 'inspect', return_value=self.inspect()):
                stream = io.StringIO()
                with contextlib.redirect_stdout(stream): result = p.main()
                return result, json.loads(stream.getvalue())
        self.assertEqual(invoke()[0], 2)
        result, report = invoke('--config-only')
        self.assertEqual(result, 0)
        self.assertFalse(report['private_https_release_ready'])

    def test_real_offline_certificate_and_cli_without_database_or_container_start(self):
        key = self.secrets / 'tls.key'
        cert = self.secrets / 'tls.crt'
        key.chmod(0o600)
        cert.chmod(0o600)
        subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
                        '-subj', '/CN=devpilot.private.example', '-addext', 'subjectAltName=DNS:devpilot.private.example',
                        '-keyout', str(key), '-out', str(cert)], check=True, capture_output=True)
        key.chmod(0o444)
        cert.chmod(0o444)
        before = {f: f.read_bytes() for f in self.base.rglob('*') if f.is_file()}
        def invoke(*extra):
            env = dict(os.environ, PYTHONPYCACHEPREFIX=str(self.base / 'forbidden-bytecode-cache'))
            env.pop('PYTHONDONTWRITEBYTECODE', None)
            result = subprocess.run(['sh', str(ROOT / 'scripts/private-release.sh'), '--manifest', str(self.manifest), *extra], capture_output=True, timeout=15, env=env)
            return result.returncode, json.loads(result.stdout)
        code, report = invoke('--config-only')
        self.assertEqual(code, 0)
        self.assertTrue(report['configuration_structure_valid'])
        self.assertFalse(report['private_https_release_ready'])
        self.assertFalse(report['key_pair_match_verified'])
        self.assertFalse(report['sql_applied'])
        self.assertEqual(invoke()[0], 2)
        result = subprocess.run(['sh', str(ROOT / 'scripts/private-release.sh'), '--manifest', str(self.manifest), '--apply'], capture_output=True, timeout=15)
        self.assertEqual(result.returncode, 2)
        self.assertEqual(before, {f: f.read_bytes() for f in self.base.rglob('*') if f.is_file()})

    def test_host_inspection_is_metadata_only_and_refuses_desktop_or_remote_engine(self):
        volume = self.base / 'volume'
        data = volume / '18/docker'
        for d in (data / 'base', data / 'pg_wal', data / 'pg_tblspc', self.base / 'docker-logs'):
            d.mkdir(parents=True)
        logs = self.base / 'docker-logs'
        self.runtime['metadata_mounts'] = [['pg-volume', '/storage'], [str(logs), '/logs-metadata']]
        pg = dict(Config=dict(Image=p.PG18), State=dict(Running=True),
                  NetworkSettings=dict(Networks={'devpilot-private-control': {}}),
                  HostConfig=dict(PortBindings={}, LogConfig=dict(Type='json-file')),
                  Mounts=[dict(Destination='/var/lib/postgresql', Type='volume', Name='pg-volume', Source=str(volume))], LogPath=str(logs / 'log.json'))
        def runner(args):
            self.commands.append(args)
            self.assertFalse({'run', 'exec', 'up', 'create', 'mount', 'chmod', 'psql', 'nft'} & set(args))
            self.assertNotIn('.Env', ' '.join(args))
            if args[:3] == ['docker', 'context', 'inspect']: return b'unix:///var/run/docker.sock\n'
            if args[:2] == ['docker', 'info']: return b'{"os":"linux","system":"Ubuntu"}'
            if args[:3] == ['ip', '-j', 'addr']: return b'[{"addr_info":[{"local":"10.44.0.1"}]}]'
            if args[:4] == ['ip', '-d', '-j', 'link']: return b'[{"linkinfo":{"info_kind":"wireguard"}}]'
            if args[:3] == ['docker', 'network', 'inspect']: return b'[{"Internal":true,"EnableIPv6":true,"Ingress":false}]'
            if args[:2] == ['docker', 'inspect']: return json.dumps(pg).encode()
            self.fail('unexpected inspection command')
        with mock.patch.object(p.platform, 'system', return_value='Linux'), mock.patch.dict(os.environ, {}, clear=True):
            value = p.host(self.runtime, self.state, '10.44.0.1', 'wg0', runner)
            self.assertFalse(value['firewall_verified'])
            self.assertFalse(value['sql_topology_verified'])
            with mock.patch.dict(os.environ, {'DOCKER_HOST': 'tcp://unapproved.example:2375'}):
                with self.assertRaises(ValueError): p.host(self.runtime, self.state, '10.44.0.1', 'wg0', runner)
        with mock.patch.object(p.platform, 'system', return_value='Darwin'):
            with self.assertRaises(ValueError): p.host(self.runtime, self.state, '10.44.0.1', 'wg0', runner)


if __name__ == '__main__':
    unittest.main()
