#!/usr/bin/env python3
"""Read-only release preparation. Never provisions, migrates, starts or opens egress."""
import argparse
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import platform
import re
import stat
import subprocess
import sys

sys.dont_write_bytecode = True

ROOT = Path(__file__).resolve().parents[2]
VERSIONS = list(range(1, 11)) + [12, 13]
PG18 = 'postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336'
BLOCKERS = [
    {'id': 'P0-1', 'kind': 'implementation', 'code': 'real_target_admission_dns_and_routed_egress_not_wired'},
    {'id': 'P0-2', 'kind': 'authorization', 'code': 'production_schema_logins_registration_and_gate_not_approved'},
    {'id': 'P1-3', 'kind': 'host_acceptance', 'code': 'dedicated_linux_vpn_tls_firewall_storage_and_key_boundaries_unverified'},
]


class HostCheckError(ValueError):
    """Fixed diagnostic codes only; never daemon output or user secret values."""


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def abspath(base, value):
    if not isinstance(value, str) or not value:
        raise ValueError('path required')
    return Path(os.path.abspath(base / value))


def private_directory(path):
    s = path.lstat()
    return stat.S_ISDIR(s.st_mode) and s.st_uid == os.getuid() and stat.S_IMODE(s.st_mode) == 0o700


def mounted_secret(path, uid):
    # No content reads. Compose file binds rely on a private HOST parent, then
    # expose only that one read-only file to its numeric container identity.
    s = path.lstat()
    return (private_directory(path.parent) and stat.S_ISREG(s.st_mode)
            and s.st_uid in (os.getuid(), uid) and not s.st_mode & 0o022
            and (s.st_mode & 0o004 or s.st_uid == uid and s.st_mode & 0o400))


def run(args):
    # Fixed read-only argv from the call sites below; no shell, URL fetch, SQL,
    # stdin credentials, Docker run/exec/up, ACL, nft or route mutations.
    p = subprocess.run(args, capture_output=True, timeout=10, stdin=subprocess.DEVNULL)
    if p.returncode:
        raise ValueError('inspection unavailable')
    return p.stdout


def schema(root):
    lock = json.loads((root / 'deploy/release/release-lock.json').read_text())
    paths = sorted((root / 'migrations').glob('*.sql'))
    if [int(p.name.split('_', 1)[0]) for p in paths] != VERSIONS:
        raise ValueError('migration inventory')
    if [p.relative_to(root).as_posix() for p in paths] != [r['path'] for r in lock['order'][:12]]:
        raise ValueError('migration filenames')
    if [r['version'] for r in lock['order']] != VERSIONS + [14, 15]:
        raise ValueError('forward order')
    for item in lock['order'] + [lock['identities']]:
        if hashlib.sha256((root / item['path']).read_bytes()).hexdigest() != item['sha256']:
            raise ValueError('source hash')
    for path in ('deploy/privateloop/private-mode-diff.json', 'deploy/capacity/permission-diff.json'):
        manifest = json.loads((root / path).read_text())
        for item in manifest.get('functions', manifest.get('existing_functions', [])):
            if hashlib.sha256((root / item['source']).read_bytes()).hexdigest() != item['source_sha256']:
                raise ValueError('candidate source dependency')
    if lock['production_activation'] or lock['new_gate_values'] or lock['new_function_execute_grants'] or lock['new_table_column_grants']:
        raise ValueError('activation unexpectedly included')
    return lock['order']


def journal(path):
    if not private_directory(path):
        raise ValueError('private journal required')
    # Presence is NOT signature verification or a release/Result acknowledgement.
    result = dict(recovery_required=False, pending_reconciliation=0, signatures_verified=False, recovery_clearance_verified=True)
    status = path / 'status.json'
    if status.is_symlink():
        raise ValueError('journal symlink')
    if status.exists() and json.loads(status.read_text()).get('event') == 'recovery_required':
        result['recovery_required'] = True
        result['recovery_clearance_verified'] = False
    for burned in path.iterdir():
        if re.fullmatch('[0-9a-f]{8}-[0-9a-f-]{27}', burned.name):
            result['recovery_clearance_verified'] = False
        if re.fullmatch('[0-9a-f]{8}-[0-9a-f-]{27}', burned.name) and not (path / (burned.name + '.owner.json')).is_file():
            result['recovery_required'] = True
    for owner in path.glob('*.owner.json'):
        result['recovery_clearance_verified'] = False
        if owner.is_symlink():
            raise ValueError('journal symlink')
        record = json.loads(owner.read_text())
        run_id = record['line']['run_id']
        if not re.fullmatch('[0-9a-f]{8}-[0-9a-f-]{27}', run_id) or owner.name != run_id + '.owner.json' or record['index'] not in (0, 1):
            raise ValueError('invalid journal identity')
        if any(not (path / (run_id + ext)).is_file() or (path / (run_id + ext)).is_symlink() for ext in ('.receipt', '.proofs')):
            result['recovery_required'] = True
        elif not (path / (run_id + '.recovered')).exists():
            result['pending_reconciliation'] += 1
    key = (path / 'private.pem').lstat()
    if not stat.S_ISREG(key.st_mode) or key.st_uid != os.getuid() or stat.S_IMODE(key.st_mode) != 0o600:
        raise ValueError('key metadata')
    pub = (path / 'public.pem').lstat()
    if not stat.S_ISREG(pub.st_mode) or pub.st_uid != os.getuid() or stat.S_IMODE(pub.st_mode) != 0o444:
        raise ValueError('public key metadata')
    return result


def native_engine(runner):
    if platform.system() != 'Linux' or os.environ.get('DOCKER_HOST'):
        raise HostCheckError('native_local_linux_required')
    endpoint = runner(['docker', 'context', 'inspect', '--format', '{{.Endpoints.docker.Host}}']).decode().strip()
    info = json.loads(runner(['docker', 'info', '--format', '{"os":{{json .OSType}},"system":{{json .OperatingSystem}}}']))
    if not endpoint.startswith('unix://') or info['os'] != 'linux' or 'desktop' in info['system'].lower():
        raise HostCheckError('native_local_linux_required')
    return dict(architecture=platform.machine(), kernel=platform.release(), engine='local-native-linux')


def host(config, state, bind_ip, vpn, runner):
    native_engine(runner)
    addresses = json.loads(runner(['ip', '-j', 'addr', 'show', 'dev', vpn]))
    links = json.loads(runner(['ip', '-d', '-j', 'link', 'show', 'dev', vpn]))
    if not any(a['local'] == bind_ip for v in addresses for a in v.get('addr_info', [])) or not any(v.get('linkinfo', {}).get('info_kind') in ('wireguard', 'tun') for v in links):
        raise HostCheckError('vpn_interface_binding_unverified')
    network = json.loads(runner(['docker', 'network', 'inspect', config['control_network']]))[0]
    if not network['Internal'] or not network['EnableIPv6'] or network['Ingress']:
        raise HostCheckError('internal_dualstack_control_network_required')
    # Select metadata only: never request Env or command/credential fields.
    template = '{"Config":{"Image":{{json .Config.Image}}},"State":{"Running":{{json .State.Running}}},"NetworkSettings":{"Networks":{{json .NetworkSettings.Networks}}},"HostConfig":{"PortBindings":{{json .HostConfig.PortBindings}},"LogConfig":{"Type":{{json .HostConfig.LogConfig.Type}}}},"Mounts":{{json .Mounts}},"LogPath":{{json .LogPath}}}'
    pg = json.loads(runner(['docker', 'inspect', '--format', template, config['pg_container']]))
    if pg['Config']['Image'] != PG18 or not pg['State']['Running'] or set(pg['NetworkSettings']['Networks']) != {config['control_network']} or pg['HostConfig']['PortBindings']:
        raise HostCheckError('pg18_control_only_container_required')
    volumes = [m for m in pg['Mounts'] if m['Destination'] == '/var/lib/postgresql']
    if len(volumes) != 1 or volumes[0]['Type'] != 'volume' or pg['HostConfig']['LogConfig']['Type'] not in ('json-file', 'local') or not pg['LogPath']:
        raise HostCheckError('unsupported_storage_topology')
    expected = [[volumes[0]['Name'], '/storage'], [str(Path(pg['LogPath']).parent), '/logs-metadata']]
    if config['metadata_mounts'] != expected:
        raise HostCheckError('storage_metadata_mounts_mismatch')
    data = Path(volumes[0]['Source']) / '18/docker'
    dirs = {'pgdata': data, 'wal': data / 'pg_wal', 'default_temp': data / 'base', 'docker_logs': Path(pg['LogPath']).parent, 'journal': state}
    if any(p.is_symlink() or not p.is_dir() for p in dirs.values()) or any((data / 'pg_tblspc').iterdir()):
        raise HostCheckError('external_or_unmapped_storage_topology')
    devices = {k: f'{os.major(p.stat().st_dev)}:{os.minor(p.stat().st_dev)}' for k, p in dirs.items()}
    if len({devices[k] for k in ('pgdata', 'wal', 'default_temp')}) != 1 or devices['journal'] not in (devices['pgdata'], devices['docker_logs']):
        raise HostCheckError('unbound_journal_wal_or_temp_device')
    return dict(architecture=platform.machine(), kernel=platform.release(), devices=devices,
                sql_topology_verified=False, canonical_fsid_verified=False,
                vpn_peer_reachability_verified=False, firewall_verified=False,
                private_key_material_read=False)


def inspect(manifest_path, inspect_host=False, runner=run, root=ROOT):
    checks = []
    report = dict(scope='release-preparation', private_https_release_ready=False,
                  real_https_execution_supported=False, production_gate_observed='unverified',
                  sql_identity_and_registry_verified=False, key_pair_match_verified=False, certificate_chain_trust_verified=False,
                  processes_started=False, sql_applied=False, egress_opened=False,
                  blockers=BLOCKERS, checks=checks)

    def check(name, action):
        try:
            value = action()
            if value is False:
                raise ValueError('check failed')
            checks.append(dict(name=name, status='pass'))
            return value
        except HostCheckError as error:
            checks.append(dict(name=name, status='fail', code=error.args[0]))
            return None
        except (OSError, ValueError, KeyError, TypeError, AssertionError, subprocess.SubprocessError):
            checks.append(dict(name=name, status='fail', code=name + '_invalid_or_unverified'))
            return None

    chain = check('source_and_forward_order', lambda: schema(root))
    if chain:
        report['schema_order'] = chain
    config = check('manifest', lambda: json.loads(manifest_path.read_text()))
    if config is None:
        return report
    if sys.flags.optimize:
        checks.append(dict(name='assertions_enabled', status='fail'))
        return report
    if set(config) != {'desired_gate', 'management_env', 'management_secrets', 'runtime_config', 'state_directory', 'vpn_interface'} or config['desired_gate'] != 'closed' or not re.fullmatch('[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}', config['vpn_interface']):
        checks.append(dict(name='manifest_boundary', status='fail'))
        return report
    base = manifest_path.parent
    env = abspath(base, config['management_env'])
    secrets = abspath(base, config['management_secrets'])
    state = abspath(base, config['state_directory'])
    renderer = module('private_ingress_config', root / 'deploy/private/configure.py')
    ingress = check('management_settings', lambda: renderer.render(env))
    values = dict(line.split('=', 1) for line in env.read_text().splitlines() if line.strip() and not line.lstrip().startswith('#')) if ingress else {}
    check('vpn_listener', lambda: bool(values) and not ipaddress.ip_address(values['PRIVATE_BIND_IP']).is_loopback)
    for filename, uid in [('migration_password', 999), ('app_password', 0), ('app_url', 65532), ('tls.key', 101), ('tls.crt', 101)]:
        check('management_' + filename.replace('.', '_'), lambda filename=filename, uid=uid: mounted_secret(secrets / filename, uid))
    if ingress:
        check('certificate_name_and_expiry', lambda: runner(['openssl', 'x509', '-in', str(secrets / 'tls.crt'), '-noout', '-checkend', '86400', '-checkhost', values['PRIVATE_HOST']]) is not None)
    sys.path.insert(0, str(root / 'runtime/private'))
    runtime = module('private_service_config', root / 'runtime/private/launcher.py')
    current = check('isolated_runtime_configuration', lambda: runtime.load(abspath(base, config['runtime_config'])))
    if current:
        check('catalog_secret_boundary', lambda: all(mounted_secret(Path(current['catalog_file']), uid) for uid in (65532, 65533)))
        for name, profile, uid in [('worker0', current['workers'][0], 65532), ('worker1', current['workers'][1], 65533), ('supervisor', current['supervisor'], 65531), ('controller', current['controller'], 65530), ('reporter', current['reporter'], 65529)]:
            check(name + '_secret_boundary', lambda profile=profile, uid=uid: mounted_secret(Path(profile['database_url_file']), uid))
    status = check('journal_metadata', lambda: journal(state))
    if status:
        report['journal'] = status
        check('recovery_guard', lambda: not status['recovery_required'])
    if inspect_host and current and values:
        observed = check('native_host', lambda: host(current, state, values['PRIVATE_BIND_IP'], config['vpn_interface'], runner))
        if observed:
            report['host'] = observed
    report['configuration_structure_valid'] = bool(checks) and all(c['status'] == 'pass' for c in checks)
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manifest', type=Path, required=True)
    parser.add_argument('--inspect-host', action='store_true', help='bounded read-only local Linux/Docker metadata')
    parser.add_argument('--config-only', action='store_true', help='exit0 means structure only; NEVER release approval')
    args = parser.parse_args()
    try:
        report = inspect(args.manifest.absolute(), args.inspect_host)
    except (OSError, ValueError, KeyError, TypeError, AssertionError, subprocess.SubprocessError):
        report = dict(private_https_release_ready=False, processes_started=False, sql_applied=False,
                      egress_opened=False, checks=[dict(name='configuration', status='fail', code='invalid_or_unavailable')], blockers=BLOCKERS)
    print(json.dumps(report, indent=2))
    return 0 if args.config_only and report.get('configuration_structure_valid') else 2


if __name__ == '__main__':
    raise SystemExit(main())
