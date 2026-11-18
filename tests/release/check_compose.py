#!/usr/bin/env python3
"""Render only: does not need a daemon or create containers/networks/volumes."""
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
result = subprocess.run(['docker', 'compose', '--profile', 'tools', '--env-file', str(ROOT / 'deploy/private/private.env.example'),
    '-f', str(ROOT / 'deploy/private/compose.yaml'), '-f', str(ROOT / 'deploy/release/compose.management.yaml'),
    'config', '--format', 'json'], capture_output=True, timeout=15, check=True)
value = json.loads(result.stdout)
network = value['networks']['database']
assert network['name'] == 'devpilot-private-control' and network['internal'] and network['enable_ipv6']
assert set(value['services']) == {'postgres', 'api', 'web', 'migrate'}
assert not value['services']['postgres'].get('ports') and not value['services']['api'].get('ports')
assert value['services']['migrate']['profiles'] == ['tools']
assert value['services']['migrate']['command'] == ['check']
assert value['services']['api']['cap_drop'] == ['ALL']
assert value['services']['web']['ports'][0]['host_ip'] == '10.44.0.1'
assert set(value['services']['api']['networks']) == {'ingress', 'database'}
assert set(value['services']['postgres']['networks']) == {'database'}
assert not any('worker' in s or 'probe' in s or 'supervisor' in s for s in value['services'])
print('PASS: Compose render only; internal dual-stack PG, VPN-only ingress, tools profile, no executor or egress')

# Render the standalone, disconnected holder with a placeholder image ID.
import os
holder = subprocess.run(['docker', 'compose', '-f', str(ROOT / 'deploy/release/compose.uplink.yaml'),
                         'config', '--format', 'json'], check=True, capture_output=True,
                        env=dict(os.environ, PRIVATE_RUNTIME_IMAGE='sha256:' + 'a' * 64))
service = json.loads(holder.stdout)['services']['uplink']
assert service['network_mode'] == 'none' and service['pull_policy'] == 'never'
assert service['cap_add'] == ['NET_ADMIN'] and service['cap_drop'] == ['ALL']
assert service['read_only'] and not any(service.get(k) for k in ('privileged','ports','secrets','volumes','devices'))
print('PASS: disconnected uplink holder; no device, bind mount, credential or port')
