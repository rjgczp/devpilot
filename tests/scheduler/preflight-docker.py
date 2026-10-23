#!/usr/bin/env python3
"""Host-side trusted tool preflight. No Docker socket is ever mounted in Probe."""
import argparse
import concurrent.futures
import hashlib
import json
import pathlib
import subprocess
import time


def run(args):
    p = subprocess.run(args, text=True, capture_output=True)
    return {'command': args, 'exit_code': p.returncode, 'stdout': p.stdout, 'stderr': p.stderr}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--evidence', required=True, type=pathlib.Path)
    args = parser.parse_args()
    dest = args.evidence.resolve()
    dest.mkdir(parents=True, exist_ok=True)
    repo = pathlib.Path(__file__).resolve().parents[2]
    prefix = ['docker', 'run', '--rm', '--network', 'none', '--cap-add', 'NET_ADMIN', '--sysctl', 'net.ipv6.conf.all.disable_ipv6=0', '--memory=128m', '--cpus=1', '--pids-limit=128', '--ulimit', 'nofile=256:256', '-v', str(repo / 'tests/scheduler') + ':/runner:ro', 'devpilot-scheduler-fixture', '-c']
    command = prefix + ['python3 /runner/preflight.py']
    started = time.time()
    with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(run, command) for _ in range(2)]
        results = [f.result() for f in futures]
    for i, result in enumerate(results):
        (dest / ('preflight-' + str(i) + '.json')).write_text(json.dumps(result, indent=2) + '\n')
        assert result['exit_code'] == 0, 'preflight failed; see evidence'
    states = [json.loads(r['stdout']) for r in results]
    assert states[0]['network_namespace'] != states[1]['network_namespace'], 'concurrent Runs shared a namespace'
    missing = run(prefix + ['PATH=/missing /usr/bin/python3 /runner/preflight.py'])
    (dest / 'missing-dependency.json').write_text(json.dumps(missing, indent=2) + '\n')
    assert missing['exit_code'] != 0 and 'missing required tool' in missing['stderr'], 'missing dependency silently skipped'
    no_caps = prefix.copy()
    capability_index = no_caps.index('--cap-add')
    del no_caps[capability_index:capability_index + 2]
    no_ipv6 = [x.replace('net.ipv6.conf.all.disable_ipv6=0', 'net.ipv6.conf.all.disable_ipv6=1') for x in prefix]
    for name, disabled in [('missing-net-admin', no_caps), ('missing-ipv6', no_ipv6)]:
        result = run(disabled + ['python3 /runner/preflight.py'])
        (dest / (name + '.json')).write_text(json.dumps(result, indent=2) + '\n')
        assert result['exit_code'] != 0, name + ' silently skipped'
    image = run(['docker', 'image', 'inspect', 'devpilot-scheduler-fixture', '--format', '{{.Id}}'])
    assert image['exit_code'] == 0
    manifest = {'started_at': started, 'finished_at': time.time(), 'image': image['stdout'].strip(), 'tests': 2, 'distinct_concurrent_namespaces': True, 'missing_dependency_fails': True, 'files': {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(dest.glob('*.json')) if p.name != 'manifest.json'}}
    (dest / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    print(json.dumps(manifest, indent=2))


if __name__ == '__main__':
    main()
