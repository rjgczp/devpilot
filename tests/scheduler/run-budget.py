#!/usr/bin/env python3
"""Budget tests each own an isolated disposable PG18 cluster; never fake release.

Destroying the test cluster is fixture teardown, not Supervisor acceptance. Every
cluster retains its started/quarantined rows through evidence capture. No test
writes a physical-release timestamp/receipt to make Goose Down pass.
"""
import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import time
import uuid

p = argparse.ArgumentParser()
p.add_argument('--binary', required=True, type=Path)
p.add_argument('--evidence', required=True, type=Path)
p.add_argument('--name', required=True)
p.add_argument('--mode', required=True, choices=['normal', 'race'])
a = p.parse_args()
repo = Path(__file__).resolve().parents[2]
binary = a.binary.resolve()
assert a.binary.is_absolute() and a.evidence.is_absolute()
assert (binary.parent / 'goose').is_file()
assert re.fullmatch('[a-zA-Z0-9_-]+', a.name)
a.evidence.mkdir(parents=True, exist_ok=False)
expected = json.loads((repo / 'tests/scheduler/budget-tests.json').read_text())
assert binary.name == ('budget-pg-race.test' if a.mode == 'race' else 'budget-pg.test')
build = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
(a.evidence / 'build.txt').write_text(build)
assert 'go1.24.3' in build and 'GOOS=linux' in build
assert 'integration,schedulerfixture,budgetfixture' in build
assert ('-race=true' in build) == (a.mode == 'race')

image = 'postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336'
def runner(container):
    return ['docker', 'run', '--rm', '--network', ('container:' + container if container else 'none'), '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
            '-v', str(binary.parent) + ':/toolbin:ro', '-v', f'{repo}:{repo}:ro',
            '-e', 'PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
            '-e', 'TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable',
            '--entrypoint', '/toolbin/' + binary.name, 'devpilot-scheduler-fixture']
listing = runner(None) + ['-test.list', '^TestBudget']
result = subprocess.run(listing, capture_output=True, text=True)
(a.evidence / 'inventory.log').write_text(result.stdout + result.stderr)
(a.evidence / 'inventory-command.json').write_text(json.dumps(dict(command=listing, exit_code=result.returncode), indent=2) + '\n')
assert result.returncode == 0
listed = result.stdout.splitlines()
assert len(listed) == len(set(listed)) and set(listed) == set(expected), listed
manifest = {'mode': a.mode, 'complete': False, 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'tests': listed, 'runs': []}
for i, test in enumerate(listed):
    label = 'devpilot-budget-' + uuid.uuid4().hex[:12]
    create = ['docker', 'run', '-d', '--name', label, '--label', 'devpilot.disposable.budget=' + label,
              '--network', 'none', '--memory=512m', '--cpus=1', '--pids-limit=128',
              '-e', 'POSTGRES_USER=devpilot_fixture', '-e', 'POSTGRES_PASSWORD=devpilot_fixture_only',
              '-e', 'POSTGRES_DB=devpilot_registration_test', image, 'postgres',
              '-c', 'cluster_name=devpilot-budget-disposable', '-c', 'log_lock_waits=on', '-c', 'deadlock_timeout=100ms']
    cid = subprocess.check_output(create, text=True).strip()
    status = 1
    prefix = f'{a.name}-{i}'
    try:
        inspect = json.loads(subprocess.check_output(['docker', 'inspect', cid]))[0]
        assert inspect['Name'] == '/' + label and inspect['Config']['Labels']['devpilot.disposable.budget'] == label
        assert not inspect['HostConfig']['Privileged'] and not inspect['HostConfig']['CapAdd'] and not inspect['HostConfig']['PortBindings']
        assert set(inspect['NetworkSettings']['Networks']) == {'none'}
        deadline = time.monotonic() + 30
        while True:
            ready = subprocess.run(['docker', 'exec', cid, 'pg_isready', '-h', '127.0.0.1', '-U', 'devpilot_fixture', '-d', 'devpilot_registration_test'], capture_output=True)
            if ready.returncode == 0:
                break
            if time.monotonic() > deadline:
                raise RuntimeError('isolated PG readiness exceeded30s; no test retry')
            time.sleep(.2)
        cmd = runner(cid) + ['-test.run', '^' + test + '$', '-test.v', '-test.failfast', '-test.timeout=120s']
        status = subprocess.run([sys.executable, str(repo/'tests/scheduler/record.py'), '--evidence', str(a.evidence), '--name', prefix,
                                 '--binary', str(binary), '--binary', str(binary.parent/'goose'), '--', *cmd], cwd=repo).returncode
        if status == 0:
            log = (a.evidence / (prefix + '.log')).read_text()
            assert re.findall(r'^=== RUN   (TestBudget\w+)$', log, re.M) == [test]
            assert re.search(r'^--- PASS: ' + re.escape(test) + r' ', log, re.M)
            assert '--- SKIP:' not in log
        manifest['runs'].append({'test': test, 'container_id': cid, 'create_command': create, 'exit_code': status})
    finally:
        (a.evidence/(prefix+'-postgres.log')).write_bytes(subprocess.check_output(['docker', 'logs', cid], stderr=subprocess.STDOUT))
        (a.evidence/(prefix+'-inspect.json')).write_text(json.dumps(json.loads(subprocess.check_output(['docker', 'inspect', cid]))[0], indent=2)+'\n')
        # Capture the ledger without raw configuration, network data or secrets.
        sql = "SELECT jsonb_build_object('at',clock_timestamp(),'budgets',(SELECT jsonb_agg(to_jsonb(b)) FROM scheduler_budget_state b),'runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r),'reservations',(SELECT jsonb_agg(to_jsonb(q)) FROM scheduler_reservations q),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l),'waits',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'wait',wait_event,'blockers',pg_blocking_pids(pid))) FROM pg_stat_activity WHERE datname=current_database()))"
        receipt = subprocess.run(['docker','exec',cid,'psql','-U','devpilot_fixture','-d','devpilot_registration_test','-At','-c',sql],capture_output=True)
        (a.evidence/(prefix+'-ledger.log')).write_bytes(receipt.stdout+receipt.stderr)
        # Only destroy the exact freshly-created, labeled disposable test server.
        current = json.loads(subprocess.check_output(['docker','inspect',cid]))[0]
        assert current['Config']['Labels'].get('devpilot.disposable.budget') == label
        subprocess.run(['docker','rm','-f','-v',cid],check=True,stdout=subprocess.DEVNULL)
        (a.evidence/(a.name+'-coverage.json')).write_text(json.dumps(manifest,indent=2)+'\n')
    if status:
        sys.exit(status)

assert len(manifest['runs']) == len(expected) and all(r['exit_code'] == 0 for r in manifest['runs'])
manifest['complete'] = True
(a.evidence / (a.name + '-coverage.json')).write_text(json.dumps(manifest, indent=2) + '\n')
