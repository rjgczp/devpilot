#!/usr/bin/env python3
"""Run the entire integration inventory serially in bounded partitions.

The existing real60s authorization and new real45s lease each get a process;
all other tests retain the existing120s bound. No skip or timeout relaxation.
Requires the original, pre-existing protected disposable PG18 container.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

parser = argparse.ArgumentParser()
parser.add_argument('--binary', required=True, type=Path)
parser.add_argument('--evidence', required=True, type=Path)
parser.add_argument('--name', required=True)
args = parser.parse_args()
repo = Path(__file__).resolve().parents[2]
binary = args.binary.resolve()
assert binary.parent == Path('/private/tmp/devpilot-scheduler-tools')
args.evidence.mkdir(parents=True, exist_ok=True)
command = ['docker', 'run', '--rm', '--network', 'container:devpilot-scheduler-apply-pg18',
           '-v', '/private/tmp/devpilot-scheduler-tools:/toolbin:ro',
           '-v', f'{repo}:{repo}:ro',
           '-e', 'PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
           '-e', 'TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable',
           '--entrypoint', '/toolbin/' + binary.name, 'devpilot-scheduler-fixture']
container = json.loads(subprocess.check_output(['docker','inspect','devpilot-scheduler-apply-pg18']))[0]
assert container['Id'] == 'db0774ff4cff96278db0eeab88ec0c2ed64f561a1c28d1671c200e4f31015103'
assert not container['HostConfig']['Privileged'] and not container['HostConfig']['CapAdd']
assert set(container['NetworkSettings']['Networks']) == {'devpilot-scheduler-apply-control'}
network = json.loads(subprocess.check_output(['docker','network','inspect','devpilot-scheduler-apply-control']))[0]
assert network['Internal'] and network['Id'] == '55e7af0982bd83c2b2b8171c0135e6ec6b04fe35c575bc895ccea35bbeef93bc'
listed = subprocess.run(command + ['-test.list', '^Test'], capture_output=True, text=True, check=True)
names = listed.stdout.splitlines()
assert names and all(re.fullmatch('Test[A-Za-z0-9_]+', n) for n in names), listed.stdout
assert len(names) == len(set(names))
long = 'TestCheckpointOneRealExpiryExplicitRenewal'
assert long in names
lease = 'TestCheckpointTwoUnstartedRealLeaseExpiry'
assert lease in names
assert any(n == 'TestCheckpointTwoClaimLostAcknowledgement' for n in names), 'build requires integration,schedulerfixture on Linux'
groups = [[n for n in names if n not in (long, lease)], [long], [lease]]
assert sorted(sum(groups, [])) == sorted(names)
manifest = {'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
            'inventory_command': command + ['-test.list', '^Test'], 'tests': names,
            'partitions': groups, 'coverage': 'all compiled tests exactly once; no skip',
            'reason': 'real60s authorization and original45s lease separated; existing120s process budget retained'}
(args.evidence / (args.name + '-coverage.json')).write_text(json.dumps(manifest, indent=2) + '\n')
for i, group in enumerate(groups):
    selected = '^(' + '|'.join(group) + ')$'
    run = [sys.executable, str(repo / 'tests/scheduler/record.py'), '--evidence', str(args.evidence),
           '--name', args.name + '-' + str(i), '--binary', str(binary), '--binary', str(binary.parent / 'goose'),
           '--', *command, '-test.run', selected, '-test.failfast', '-test.timeout=120s', '-test.v']
    result = subprocess.run(run, cwd=repo)
    if result.returncode:
        sys.exit(result.returncode)
