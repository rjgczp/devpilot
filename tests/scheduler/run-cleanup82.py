#!/usr/bin/env python3
"""Real signed physical release, then real one-hour cooling, then PG cleanup.

No clock changes, timestamp shortening, fabricated receipt or timeout override.
Normal/race use separate disposable clusters. Each Go phase keeps120s; waiting
holds no application connection/transaction and never reopens target permission.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor, as_completed
import hashlib
import json
from pathlib import Path
import subprocess
import threading
import time

from recovery_fixture import RecoveryHarness

cancel = threading.Event()


class CleanupHarness(RecoveryHarness):
    def phase(self, test, receipt=None):
        repo = Path(__file__).resolve().parents[2]
        cmd = ['docker', 'run', '--rm', '--network', 'container:' + self.prefix + '-pg',
               '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
               '-v', str(repo) + ':' + str(repo) + ':ro', '-v', str(self.pg_binary.parent) + ':/toolbin:ro',
               '-w', str(repo), '-e', 'PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
               '-e', 'TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable',
               '-e', 'CLEANUP82_PRESERVED_CONTROL=' + ('real-recovery-unconfirmed-v1' if self.recovery_test == 'TestRecoveryUnconfirmedPG' else 'real-recovery-pause-v1')]
        if receipt:
            # Only the signed public receipt, never the Supervisor private key.
            cmd += ['-v', str(receipt) + ':/retired-proof/old.receipt:ro']
        cmd += ['--entrypoint', '/toolbin/' + self.pg_binary.name, 'devpilot-scheduler-fixture',
                '-test.run', '^' + test + '$', '-test.v', '-test.failfast', '-test.timeout=120s']
        result = subprocess.run(cmd, capture_output=True, timeout=120)
        (self.dest / (test + '.log')).write_bytes(result.stdout + result.stderr)
        self.record(test + '-command', dict(command=cmd, exit_code=result.returncode,
                    binary_sha256=hashlib.sha256(self.pg_binary.read_bytes()).hexdigest()))
        assert result.returncode == 0, test + ' failed; no retry'
        assert b'--- PASS: ' + test.encode() in result.stdout and b'--- SKIP:' not in result.stdout
        return result.stdout.decode()

    def pg_completion(self, mode, status):
        result = super().pg_completion(mode, status)
        assert len(self.events()) == 1, 'original HTTPS receiver count changed'
        before_tcp = len([x for x in self.events('accept') if x['port'] in (80, 443)])
        assert before_tcp == 1, 'unexpected original target TCP count'
        output = self.phase('TestCleanupPhysicalPreparePG')
        markers = [json.loads(x[len('COOLING82 '):]) for x in output.splitlines() if x.startswith('COOLING82 ')]
        assert len(markers) == 1
        marker = markers[0]
        wait = marker['wait_seconds']
        assert 0 <= wait <= 3601
        start = time.monotonic()
        self.record('real-cooling-begin', dict(**marker, monotonic=start, transaction_held=False,
                                             network_reopened=False))
        while time.monotonic() - start < wait + .05:
            if cancel.wait(min(30, max(.001, wait + .05 - (time.monotonic() - start)))):
                raise RuntimeError('other independent mode failed; stop, no retry')
        self.record('real-cooling-end', dict(elapsed_seconds=time.monotonic() - start))
        receipt = self.authority.path / (marker['run_id'] + '.receipt')
        assert receipt.is_file(), 'independent signed receipt absent'
        self.phase('TestCleanupPhysicalFinishPG', receipt)
        assert len(self.events()) == 1, 'cleanup/reconciliation sent another GET'
        after_tcp = len([x for x in self.events('accept') if x['port'] in (80, 443)])
        assert after_tcp == before_tcp, 'cleanup/replay opened another target TCP'
        self.record('cleanup-target-witness', dict(target_tcp=after_tcp, get_count=1))
        result['real_cooling_seconds'] = time.monotonic() - start
        result['cleanup_verified'] = True
        return result


class UnconfirmedHarness(CleanupHarness):
    def pg_completion(self, mode, status):
        result = RecoveryHarness.pg_completion(self, mode, status)
        before = len(self.events())
        before_tcp = len([x for x in self.events('accept') if x['port'] in (80, 443)])
        self.phase('TestCleanupPhysicalRejectPG')
        assert len(self.events()) == before, 'rejected cleanup caused another GET'
        assert len([x for x in self.events('accept') if x['port'] in (80, 443)]) == before_tcp, 'rejected cleanup opened target TCP'
        result['quarantine_preserved'] = True
        return result


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--tools', required=True, type=Path)
    p.add_argument('--evidence', required=True, type=Path)
    a = p.parse_args()
    assert a.tools.is_absolute() and a.evidence.is_absolute()
    a.evidence.mkdir(parents=True, exist_ok=False)
    runs = []

    def run(mode):
        binary = a.tools / ('recovery-pg-race.test' if mode == 'race' else 'recovery-pg.test')
        info = subprocess.check_output(['go', 'version', '-m', str(binary)], text=True)
        assert 'go1.24.3' in info and 'GOOS=linux' in info and 'recoveryfixture' in info
        assert ('-race=true' in info) == (mode == 'race')
        for cls, test, case, name in [(CleanupHarness, 'TestRecoveryPausePG', 'allow-https', mode),
                                      (UnconfirmedHarness, 'TestRecoveryUnconfirmedPG', 'supervisor-killed', mode + '-unconfirmed')]:
            h = cls(a.evidence / name, a.tools / 'networksupervisorfixture',
                    a.tools / 'schedulerprobefixture', case, mode)
            h.recovery_test = test
            h.pg_binary = binary
            code = 1
            try:
                h.pg(binary, mode)
                code = 0
            except BaseException:
                cancel.set()
                raise
            finally:
                h.record('cleanup82-result', dict(mode=mode, test=test, exit_code=code, production_execution=False))
                h.close()
        return dict(mode=mode, exit_code=code, phases=5, production_execution=False)

    try:
        with ThreadPoolExecutor(max_workers=2) as pool:
            for future in as_completed([pool.submit(run, mode) for mode in ['normal', 'race']]):
                runs.append(future.result())
    finally:
        (a.evidence / 'result.json').write_text(json.dumps(dict(runs=runs,
            complete=len(runs) == 2 and all(r['exit_code'] == 0 for r in runs), retry=False), indent=2) + '\n')


if __name__ == '__main__':
    main()
