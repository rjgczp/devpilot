#!/usr/bin/env python3
"""Validate nonsecret settings and render the private TLS ingress. Starts nothing."""
import argparse
import ipaddress
import os
import re
import secrets
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent
KEYS = {'PRIVATE_BIND_IP', 'PRIVATE_HTTPS_PORT', 'PRIVATE_HOST', 'PRIVATE_ALLOWED_CIDRS'}
PRIVATE = [ipaddress.ip_network(s) for s in ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '127.0.0.0/8', 'fc00::/7', '::1/128')]


def private(network):
    return any(network.version == n.version and network.subnet_of(n) for n in PRIVATE)


def render(path):
    values = {}
    for line in path.read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#'):
            continue
        key, sep, value = line.partition('=')
        if not sep or key not in KEYS or key in values or value != value.strip():
            raise ValueError('invalid or duplicate private setting')
        values[key] = value
    if set(values) != KEYS:
        raise ValueError('all four private settings are required')
    bind = ipaddress.ip_address(values['PRIVATE_BIND_IP'])
    if bind.version != 4 or not private(ipaddress.ip_network(str(bind)+'/32')):
        raise ValueError('bind must be a specific private/VPN IPv4 or loopback address')
    port = values['PRIVATE_HTTPS_PORT']
    if not port.isascii() or not port.isdecimal() or str(int(port)) != port or not 1 <= int(port) <= 65535:
        raise ValueError('invalid HTTPS port')
    host = values['PRIVATE_HOST']
    if len(host) > 253 or not re.fullmatch(r'[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?', host) or '.' not in host:
        raise ValueError('host must be a DNS name, without scheme/port/path')
    for label in host.split('.'):
        if not 1 <= len(label) <= 63 or label.startswith('-') or label.endswith('-'):
            raise ValueError('invalid host label')
    if all(c in '0123456789.' for c in host):
        raise ValueError('host must be a DNS name')
    networks = [ipaddress.ip_network(s, strict=True) for s in values['PRIVATE_ALLOWED_CIDRS'].split(',')]
    if not 1 <= len(networks) <= 16 or any(not private(n) for n in networks):
        raise ValueError('allowlist must contain 1-16 explicit private/VPN CIDRs')
    origin = 'https://'+host+(':'+port if port != '443' else '')
    return ROOT.joinpath('nginx.conf.template').read_text().replace('@@HOST@@', host).replace('@@ORIGIN@@', origin).replace(
        '@@ALLOW@@', '\n        '.join('allow '+str(n)+';' for n in networks))


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--env', type=Path, default=ROOT/'.env')
    p.add_argument('--output', type=Path, default=ROOT/'.runtime/nginx.conf')
    p.add_argument('--init-secrets', type=Path, help='create NEW private directory with random DB secrets; never overwrite')
    args = p.parse_args()
    try:
        config = render(args.env)
    except (ValueError, OSError):
        p.exit(1, 'Invalid private configuration; check the documented nonsecret settings.\n')
    args.output.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(mode='w', dir=args.output.parent, delete=False) as output:
        output.write(config)
        output.flush()
        os.fchmod(output.fileno(), 0o644)
    os.replace(output.name, args.output)
    if args.init_secrets:
        args.init_secrets.mkdir(mode=0o700, parents=False, exist_ok=False)
        migration, app = secrets.token_hex(32), secrets.token_hex(32)
        for name, value in {
            'migration_password': migration,
            'app_password': app,
            'app_url': 'postgres://devpilot_private_app:'+app+'@postgres:5432/devpilot_private?sslmode=disable',
        }.items():
            file = args.init_secrets/name
            file.write_text(value+'\n')
            # File bind mounts must be readable by the distinct nonroot UIDs.
            # The host parent remains 0700; services get only named secret files.
            file.chmod(0o444)
    print('Private ingress configuration validated; no service started.')
