#!/usr/bin/env python3
"""Run every compiled integration test serially, with bounded complete partitions.

The new real 60s authorization test has its own process so the historical 120s
suite budget is retained. No test is skipped: inventory and coverage are checked.
The container/database are pre-existing dedicated fixtures, not app resources.
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
listed = subprocess.run(command + ['-test.list', '^Test'], capture_output=True, text=True, check=True)
names = listed.stdout.splitlines()
assert names and all(re.fullmatch('Test[A-Za-z0-9_]+', n) for n in names), listed.stdout
assert len(names) == len(set(names))
long = 'TestCheckpointOneRealExpiryExplicitRenewal'
assert long in names
groups = [[n for n in names if n != long], [long]]
assert sorted(sum(groups, [])) == sorted(names)
manifest = {'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
            'inventory_command': command + ['-test.list', '^Test'], 'tests': names,
            'partitions': groups, 'coverage': 'all compiled tests exactly once; no skip',
            'reason': 'real minimum60s expiry separated; existing per-process120s retained'}
(args.evidence / (args.name + '-coverage.json')).write_text(json.dumps(manifest, indent=2) + '\n')
for i, group in enumerate(groups):
    selected = '^(' + '|'.join(group) + ')$'
    run = [sys.executable, str(repo / 'tests/scheduler/record.py'), '--evidence', str(args.evidence),
           '--name', args.name + '-' + str(i), '--binary', str(binary), '--binary', str(binary.parent / 'goose'),
           '--', *command, '-test.run', selected, '-test.failfast', '-test.timeout=120s', '-test.v']
    result = subprocess.run(run, cwd=repo)
    if result.returncode:
        sys.exit(result.returncode)
