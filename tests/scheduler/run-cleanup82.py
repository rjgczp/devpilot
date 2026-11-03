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


def public_receipt_input(source, destination):
    # Authority's private journal remains0600. Export only the already-public
    # signed receipt, byte-for-byte, never its directory or signing key.
    metadata = source.stat()
    assert metadata.st_mode & 0o777 == 0o600
    with source.open('rb') as stream:
        raw = stream.read(65537)
    assert 0 < len(raw) <= 65536
    with destination.open('xb') as stream:
        stream.write(raw)
    destination.chmod(0o444)
    cmd = ['docker', 'run', '--rm', '--network', 'none', '--read-only',
           '--user', '65534:65534', '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
           '--memory=128m', '--cpus=1', '--pids-limit=128', '--ulimit', 'nofile=256:256',
           '-v', str(destination) + ':/proof/public.receipt:ro',
           '--entrypoint', 'python3', 'devpilot-scheduler-fixture', '-S', '-c', '''
import errno,hashlib,json,os,pathlib
p=pathlib.Path('/proof/public.receipt')
assert os.getuid()==65534
status=dict(line.split(':',1) for line in pathlib.Path('/proc/self/status').read_text().splitlines() if ':' in line)
assert int(status['CapEff'].strip(),16)==0 and status['NoNewPrivs'].strip()=='1'
assert p.stat().st_mode&0o777==0o444
raw=p.read_bytes()
try:p.open('wb')
except OSError as error:assert error.errno in (errno.EACCES,errno.EROFS)
else:raise AssertionError('public receipt writable')
print(json.dumps(dict(uid=os.getuid(),public_uid=p.stat().st_uid,public_mode=oct(p.stat().st_mode&0o777),
 cap_eff=status['CapEff'].strip(),no_new_privs=1,sha256=hashlib.sha256(raw).hexdigest(),public_read_only=True)))
''']
    try:
        result = subprocess.run(cmd, capture_output=True, timeout=30)
    except subprocess.TimeoutExpired as error:
        destination.with_suffix('.input.log').write_bytes((error.stdout or b'') + (error.stderr or b''))
        raise
    destination.with_suffix('.input.log').write_bytes(result.stdout + result.stderr)
    assert result.returncode == 0, 'public receipt input failed; no retry'
    witness = json.loads(result.stdout)
    assert witness['sha256'] == hashlib.sha256(raw).hexdigest()
    assert source.stat().st_mode == metadata.st_mode and source.read_bytes() == raw
    return dict(command=cmd, exit_code=result.returncode, source_uid=metadata.st_uid,
                source_mode=oct(metadata.st_mode & 0o777), **witness)


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
        try:
            result = subprocess.run(cmd, capture_output=True, timeout=120)
        except subprocess.TimeoutExpired as error:
            (self.dest / (test + '.log')).write_bytes((error.stdout or b'') + (error.stderr or b''))
            self.record(test + '-command', dict(command=cmd, exit_code=124, timed_out=True,
                        binary_sha256=hashlib.sha256(self.pg_binary.read_bytes()).hexdigest()))
            raise
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
        source = self.authority.path / (marker['run_id'] + '.receipt')
        assert source.is_file(), 'independent signed receipt absent'
        receipt = self.dest / 'retired-public.receipt'
        self.record('public-receipt-input', public_receipt_input(source, receipt))
        wait = marker['wait_seconds']
        assert 0 <= wait <= 3601
        start = time.monotonic()
        self.record('real-cooling-begin', dict(**marker, monotonic=start, transaction_held=False,
                                             network_reopened=False))
        while time.monotonic() - start < wait + .05:
            if cancel.wait(min(30, max(.001, wait + .05 - (time.monotonic() - start)))):
                raise RuntimeError('other independent mode failed; stop, no retry')
        self.record('real-cooling-end', dict(elapsed_seconds=time.monotonic() - start))
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
