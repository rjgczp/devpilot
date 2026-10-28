#!/usr/bin/env python3
"""Serial PG18 CI groups with complete, explicit inventory and unchanged120s cap."""
import argparse
from collections import Counter
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = Path(__file__).with_name('integration-groups.json')
TEST_NAME = re.compile(r'(?:Test|Fuzz|Example)[A-Za-z0-9_]*\Z')


def check_plan(groups, discovered):
    names = [name for group in groups for name in group['tests']]
    if any(not TEST_NAME.fullmatch(name) for name in names):
        raise ValueError('invalid exact test name')
    duplicates = [name for name, count in Counter(names).items() if count != 1]
    missing = sorted(set(discovered) - set(names))
    stale = sorted(set(names) - set(discovered))
    if not groups or duplicates or missing or stale:
        raise ValueError(json.dumps({'duplicates': duplicates, 'unassigned': missing, 'stale': stale}))
    slow = {'TestCheckpointOneRealExpiryExplicitRenewal', 'TestCheckpointTwoUnstartedRealLeaseExpiry'}
    if not slow <= set(names) or any(len(group['tests']) != 1 for group in groups if slow.intersection(group['tests'])):
        raise ValueError('real60s authorization and45s Lease must each own a process')
    return names


def listed_tests(log):
    found = {}
    for line in log.splitlines():
        event = json.loads(line)
        if event['Action'] == 'output':
            name = event.get('Output', '').strip()
            if TEST_NAME.fullmatch(name):
                found.setdefault(event['Package'], []).append(name)
    return found


def check_results(log, expected):
    runs, passed, packages = Counter(), Counter(), set()
    for line in log.splitlines():
        event = json.loads(line)
        pkg, name = event.get('Package'), event.get('Test')
        if event['Action'] == 'skip' and (name or expected.get(pkg)):
            raise ValueError('a required test was skipped')
        if name and '/' not in name:
            if event['Action'] == 'run':
                runs[pkg, name] += 1
            if event['Action'] == 'pass':
                passed[pkg, name] += 1
        if event['Action'] == 'pass' and not name:
            packages.add(pkg)
    wanted = Counter({(pkg, name): 1 for pkg, names in expected.items() for name in names})
    if runs != wanted or passed != wanted or not {pkg for pkg, names in expected.items() if names} <= packages:
        raise ValueError('executed/passed inventory differs from the exact plan')
    return sum(passed.values())


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--mode', choices=['normal', 'race'], required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--plan-only', action='store_true')
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    evidence.mkdir(parents=True, exist_ok=False)
    manifest = json.loads(MANIFEST.read_text())
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    expected_head = os.environ.get('CI_EXPECTED_HEAD')
    if expected_head and head != expected_head:
        raise ValueError('checkout does not match the frozen push/PR head')
    run_url = f"{os.environ.get('GITHUB_SERVER_URL', 'https://github.com')}/{os.environ.get('GITHUB_REPOSITORY', 'rjgczp/devpilot')}/actions/runs/{os.environ.get('GITHUB_RUN_ID', 'local')}"
    result = {'head': head, 'mode': args.mode, 'run_url': run_url, 'groups': [], 'complete': False}
    (evidence / 'result.json').write_text(json.dumps(result, indent=2) + '\n')

    def command(label, argv):
        print(label, flush=True)
        receipt = [sys.executable, str(ROOT / 'tests/scheduler/record.py'), '--evidence', str(evidence), '--name', label, '--', *argv]
        status = subprocess.run(receipt, cwd=ROOT).returncode
        log = (evidence / (label + '.log')).read_text()
        if status:
            print(log, file=sys.stderr)
            raise SystemExit(status)
        return log

    # Listing invokes compiled Go tests, not a source regex, and runs no test body.
    listing = command('inventory', ['go', 'test', '-tags=' + manifest['tags'], '-json', '-list', '.', './...'])
    inventory = listed_tests(listing)
    package = manifest['package']
    planned = check_plan(manifest['groups'], inventory.get(package, []))
    plan = {'head': head, 'tags': manifest['tags'], 'discovered': inventory, 'groups': manifest['groups'], 'pg_test_count': len(planned)}
    (evidence / 'plan.json').write_text(json.dumps(plan, indent=2) + '\n')
    if args.plan_only:
        return
    flags = ['-race'] if args.mode == 'race' else []
    for group in manifest['groups']:
        label = group['name']
        exact = '^(' + '|'.join(re.escape(name) for name in group['tests']) + ')$'
        argv = ['go', 'test', *flags, '-tags=' + manifest['tags'], '-json', './tests/integration', '-run', exact, '-count=1', '-timeout=120s']
        log = command(label, argv)
        count = check_results(log, {package: group['tests']})
        result['groups'].append({'name': label, 'count': count, 'exit_code': 0, 'command': argv})
        (evidence / 'result.json').write_text(json.dumps(result, indent=2) + '\n')
    # Preserve all other packages formerly included by go test -tags=integration ./....
    packages = subprocess.check_output(['go', 'list', '-tags=' + manifest['tags'], './...'], cwd=ROOT, text=True).splitlines()
    other = [pkg for pkg in packages if pkg != package]
    argv = ['go', 'test', *flags, '-tags=' + manifest['tags'], '-json', *other, '-count=1', '-timeout=120s']
    log = command('other-packages', argv)
    result['other_test_count'] = check_results(log, {pkg: inventory.get(pkg, []) for pkg in other})
    result['pg_test_count'] = len(planned)
    result['complete'] = True
    (evidence / 'result.json').write_text(json.dumps(result, indent=2) + '\n')


if __name__ == '__main__':
    main()
