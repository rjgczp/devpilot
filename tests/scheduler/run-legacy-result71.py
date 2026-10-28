#!/usr/bin/env python3
"""Immutable 7.1 compatibility regression in a separate legacy schema8 fixture.

The current checkout orchestrates; historical binaries, migrations, assertions,
Go helpers and network harness all come from the pinned commit. Every inner
receipt truthfully identifies that historical component, not the current SHA.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys

LEGACY = 'c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c'
REPO = Path(__file__).resolve().parents[2]


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


def identity(root):
    assert git(root, 'rev-parse', 'HEAD') == LEGACY
    assert not git(root, 'status', '--porcelain'), 'historical source was modified'
    files = git(root, 'ls-files', '-z').split('\0')
    hashes = {p: hashlib.sha256((root / p).read_bytes()).hexdigest()
              for p in sorted(files) if p and (root / p).is_file()}
    return dict(component_sha=LEGACY, tree=git(root, 'rev-parse', 'HEAD^{tree}'),
                schema=8, result_protocol='legacy-single-attempt', sources=hashes,
                source_inventory_sha256=hashlib.sha256(json.dumps(hashes, sort_keys=True).encode()).hexdigest())


def main():
    p = argparse.ArgumentParser()
    p.add_argument('action', choices=['prepare', 'build', 'run'])
    p.add_argument('--snapshot', type=Path, required=True)
    p.add_argument('--tools', type=Path, required=True)
    p.add_argument('--evidence', type=Path, required=True)
    a = p.parse_args()
    assert all(x.is_absolute() for x in [a.snapshot, a.tools, a.evidence])
    assert not a.snapshot.is_relative_to(REPO) and a.snapshot != REPO
    a.evidence.mkdir(parents=True, exist_ok=True)
    if a.action == 'prepare':
        assert not a.snapshot.exists(), 'refuse to replace an existing snapshot'
        assert git(REPO, 'rev-parse', LEGACY+'^{commit}') == LEGACY
        subprocess.run(['git', 'merge-base', '--is-ancestor', LEGACY, 'HEAD'], cwd=REPO, check=True)
        subprocess.run(['git', 'clone', '--shared', '--no-checkout', '--', str(REPO), str(a.snapshot)], check=True)
        subprocess.run(['git', '-C', str(a.snapshot), 'checkout', '--detach', LEGACY], check=True)
    baseline = identity(a.snapshot)
    proof = dict(baseline)
    proof.update(orchestrator_sha=git(REPO, 'rev-parse', 'HEAD'), production_execution=False,
                 assertions_modified=False, role_expansion=False)
    (a.evidence / (a.action+'-source.json')).write_text(json.dumps(proof, indent=2)+'\n')
    code = 0
    if a.action == 'build':
        a.tools.mkdir(parents=True, exist_ok=False)
        goose = shutil.which('goose')
        assert goose and '3.26.0' in subprocess.check_output([goose, '-version'], text=True, stderr=subprocess.STDOUT)
        shutil.copy2(goose, a.tools/'goose')
        env = dict(os.environ, RESULT71_TOOLS=str(a.tools))
        code = subprocess.run(['sh', 'tests/scheduler/build-result71.sh'], cwd=a.snapshot, env=env).returncode
        if not code:
            hashes = {p.name: hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(a.tools.iterdir()) if p.is_file()}
            (a.evidence/'binaries.json').write_text(json.dumps(hashes, indent=2)+'\n')
    elif a.action == 'run':
        expected = json.loads((a.evidence/'binaries.json').read_text())
        assert all(hashlib.sha256((a.tools/n).read_bytes()).hexdigest()==h for n,h in expected.items())
        code = subprocess.run([sys.executable, '-B', str(a.snapshot/'tests/scheduler/run-result71.py'),
                               '--tools', str(a.tools), '--evidence', str(a.evidence/'matrix')], cwd=a.snapshot).returncode
        if not code:
            result = json.loads((a.evidence/'matrix/result.json').read_text())
            assert result['complete'] and len(result['runs'])==24 and all(x['exit_code']==0 for x in result['runs'])
    assert identity(a.snapshot) == baseline, 'historical source changed during command'
    (a.evidence/(a.action+'-result.json')).write_text(json.dumps(dict(exit_code=code,component_sha=LEGACY,
        orchestrator_sha=proof['orchestrator_sha'],production_execution=False), indent=2)+'\n')
    return code


if __name__ == '__main__':
    raise SystemExit(main())
