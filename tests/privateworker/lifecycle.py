#!/usr/bin/env python3
"""New private command against the existing guarded no-upstream PG18 fixture."""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

sys.path.insert(0, str(Path(__file__).resolve().parents[1]/'scheduler'))
from periodic91 import PeriodicHarness
from network_fixture import IMAGE, LABEL


class PrivateRuntimeHarness(PeriodicHarness):
    def __init__(self, dest, tools):
        super().__init__(dest, tools, 'normal')
        self.pg_test_pattern = 'TestPrivateWorkerLifecyclePG'
        self.runtime_verified = False

    def pg_operation(self, instance, op, payload, selected, mode):
        if op != 'worker-private-runtime':
            return super().pg_operation(instance, op, payload, selected, mode)
        self.receive_events()
        before = self.events('get')
        assert len(before) == 1, 'an actual in-flight fixture GET is required'
        with tempfile.TemporaryDirectory(prefix='devpilot-worker-secret-') as root:
            os.chmod(root, 0o700)
            file = Path(root)/'database_url'
            file.write_text(payload['DSN']+'\n')
            file.chmod(0o444)
            for index in range(2):
                name = self.prefix+'-private-worker-'+str(index)
                cmd = ['docker', 'run', '-i', '--name', name, '--label', LABEL+'='+self.prefix,
                       '--network', self.control, '--user', '65534:65534', '--read-only',
                       '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
                       '--memory=256m', '--memory-swap=256m', '--cpus=1', '--pids-limit=128',
                       '--ulimit', 'nofile=256:256',
                       '-e', 'DEVPILOT_EXECUTION_GATE=fixture', '-e', 'DEVPILOT_PRIVATE_GATE=private',
                       '-v', str(self.tools)+':/toolbin:ro', '-v', str(file)+':/run/worker-dsn:ro',
                       '--entrypoint', '/toolbin/privateworker', IMAGE,
                       '--database-url-file', '/run/worker-dsn', '--incarnation', payload['Incarnation']]
                # Only a Worker URL secret is mounted; no source, API/admin
                # credential, management socket or Supervisor private key.
                proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                self.processes.append(proc)
                self.control_ids.add(name); self.containers.append(name)
                try:
                    ready = json.loads(self.read_bytes(proc, time.monotonic()+10))
                except Exception:
                    if proc.poll() is None:
                        subprocess.run(['docker','kill',name],capture_output=True)
                    out, err = proc.communicate(timeout=5)
                    self.record('private-worker-startup-error', dict(exit_code=proc.returncode, error=err.decode(errors='replace')[:500]))
                    raise
                assert ready == dict(event='worker_ready', gate='closed', execution_available=False)
                info = json.loads(self.run(['docker', 'inspect', name]))[0]
                assert not info['HostConfig']['Privileged'] and not info['HostConfig']['CapAdd']
                assert not info['HostConfig']['PortBindings']
                assert set(info['NetworkSettings']['Networks']) == {self.control}
                self.run(['docker', 'kill', '--signal', 'TERM', name])
                out, err = proc.communicate(timeout=8)
                assert proc.returncode == 0 and not err, 'new Worker must exit cleanly'
                assert json.loads(out) == dict(event='worker_stopped', gate='closed', physical_release=False)
                self.record('closed-private-worker-'+str(index), dict(started=True, stopped=True, gate='closed', restarted=index==1, physical_release=False))
        self.receive_events()
        assert self.events('get') == before, 'closed Worker exit/restart must not resend GET'
        self.runtime_verified = True
        return dict(event='private_runtime_closed')


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--tools', type=Path, required=True)
    p.add_argument('--evidence', type=Path, required=True)
    args = p.parse_args()
    h = PrivateRuntimeHarness(args.evidence.resolve(), args.tools.resolve())
    status = 1
    try:
        h.pg(h.tools/'private-worker-pg.test', 'normal')
        assert h.runtime_verified, 'new private executable was not exercised'
        status = 0
    finally:
        h.record('runner-exit', dict(exit_code=status, private_worker=True, execution_gate='closed', public_execution=False))
        h.containers = [cid for cid in h.containers if subprocess.run(['docker','inspect',cid],capture_output=True).returncode==0]
        h.control_ids = set()
        h.close()
    return status


if __name__ == '__main__':
    raise SystemExit(main())
