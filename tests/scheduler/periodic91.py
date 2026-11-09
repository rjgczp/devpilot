#!/usr/bin/env python3
"""Task9.1 only: original kernel/physical Supervisor, two private PG Workers.

The host retains Docker/signing authority. Control containers have no target
network; Probe containers have no control bridge, source or DB credentials.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import select
import secrets
import subprocess
import time

from network_fixture import HERE, IMAGE, LABEL, now_string
from recovery_fixture import RecoveryHarness

PG = 'postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336'


class PeriodicHarness(RecoveryHarness):
    def __init__(self, dest, tools, mode):
        super().__init__(dest, tools/'networksupervisorfixture', tools/'schedulerprobefixture', 'allow-https', mode)
        self.tools = tools
        self.workers = []
        self.control_ids = set()
        self.completed = []
        self.worker_log = []
        self.recovery_test = 'TestPeriodicSchedulerPG'
        self.pg_password = secrets.token_urlsafe(32)

    def record(self, name, value):
        # Ephemeral cluster bootstrap credentials stay outside exported logs.
        raw = json.dumps(value)
        password = getattr(self, 'pg_password', None)
        if password:
            raw = raw.replace(password, '<ephemeral-password-redacted>')
        return super().record(name, json.loads(raw))

    def assert_wires(self):
        # Preserve the original no-upstream assertions for every data-plane
        # namespace. Only explicitly registered PG/control containers are exempt
        # from network=none, and those must use our one internal bridge.
        all_ids = self.containers
        self.containers = [cid for cid in all_ids if cid not in self.control_ids]
        try:
            super().assert_wires()
        finally:
            self.containers = all_ids
        for cid in self.control_ids:
            info = json.loads(self.run(['docker', 'inspect', cid]))[0]
            assert set(info['NetworkSettings']['Networks']) == {self.control}
            assert info['HostConfig']['NetworkMode'] == self.control
            assert not info['HostConfig']['Privileged'] and not info['HostConfig']['PortBindings']
            if cid == getattr(self, 'pg_id', None):
                assert {x.removeprefix('CAP_') for x in info['HostConfig']['CapAdd']} == {'CHOWN', 'DAC_OVERRIDE', 'FOWNER', 'SETUID', 'SETGID'}
            else:
                assert not info['HostConfig']['CapAdd']
            assert not any('docker.sock' in x['Source'] for x in info['Mounts'])

    def control_command(self, name, binary, test, source=False):
        cmd = ['docker', 'run', '--rm', '-i', '--name', name, '--label', LABEL+'='+self.prefix,
               '--network', self.control, '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
               '--read-only', '--memory=256m', '--memory-swap=256m', '--cpus=1', '--pids-limit=128',
               '--ulimit', 'nofile=256:256', '--tmpfs', '/tmp:rw,noexec,nosuid,size=8m',
               '-v', str(self.tools)+':/toolbin:ro', '-e', 'PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin']
        if source:
            repo = HERE.parents[1]
            # Match the source path embedded by the pinned native Linux builder.
            cmd += ['-v', str(repo)+':'+str(repo)+':ro', '-w', str(repo),
                    '-e', 'TEST_DATABASE_URL=postgres://devpilot_fixture:'+self.pg_password+'@pg:5432/devpilot_registration_test?sslmode=disable']
        else:
            cmd += ['--user', '65534:65534', '-e', 'DEVPILOT_PERIODIC_WORKER=fixture-only']
        # Keep Worker containers until label-checked outer teardown, including
        # failure paths. --rm races the original teardown's ownership check.
        if not source:
            cmd.remove('--rm')
        return cmd + ['--entrypoint', '/toolbin/'+binary, IMAGE, '-test.run', '^('+test+')$', '-test.v', '-test.timeout=300s']

    def worker_read(self, index):
        proc = self.workers[index]
        until = time.monotonic()+12
        while True:
            raw = self.read_bytes(proc, until)
            if raw.startswith(b'PERIODIC '):
                message = json.loads(raw[len(b'PERIODIC '):])
                # Frames/credentials never enter evidence. Public cycle records
                # and receiver observations are recorded separately.
                self.worker_log.append(dict(worker=index, at=now_string(), op=message['op'],
                                            bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest()))
                return message
            self.worker_log.append(dict(worker=index, line=raw.decode(errors='replace')))
            if raw.startswith((b'--- FAIL:', b'FAIL')):
                raise RuntimeError('Worker failed; see worker protocol log')

    def read_bytes(self, proc, until):
        raw = bytearray()
        while len(raw) <= 65536:
            if not select.select([proc.stdout], [], [], max(0, until-time.monotonic()))[0]:
                raise RuntimeError('bounded periodic management wait expired')
            b = os.read(proc.stdout.fileno(), 1)
            if not b:
                raise RuntimeError('periodic process exited without completion')
            raw += b
            if b == b'\n':
                return bytes(raw)
        raise RuntimeError('oversized periodic reply')

    @staticmethod
    def send(proc, value):
        proc.stdin.write(json.dumps(value).encode()+b'\n')
        proc.stdin.flush()

    def pg_operation(self, instance, op, payload, selected, mode):
        if op == 'workers-start':
            assert len(payload) == 2 and not self.workers
            for i, boot in enumerate(payload):
                name = self.prefix+'-worker'+str(i+1)
                cmd = self.control_command(name, self.pg_binary.name, 'TestPeriodicWorkerProcess')
                proc = subprocess.Popen(cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=open(self.dest/('worker'+str(i)+'.stderr'), 'wb'))
                self.processes.append(proc)
                self.workers.append(proc)
                # Startup goes only through a private pipe, never environment,
                # argv, evidence, or a data-plane mount.
                self.send(proc, boot)
                identity = self.worker_read(i)
                assert identity['op'] == 'ready'
                self.record('worker'+str(i)+'-identity', identity['payload'])
                self.run(['docker', 'inspect', name])
                self.control_ids.add(name)
                self.containers.append(name)
                self.record('worker'+str(i)+'-container', json.loads(self.run(['docker', 'inspect', name]))[0])
            self.assert_wires()
            return dict(event='workers_started')
        if op == 'workers-tick':
            for proc in self.workers:
                self.send(proc, dict(op='tick', payload=None))
            return dict(workers=[self.worker_read(i) for i in range(2)])
        if op in ('worker-call', 'worker-send'):
            i = payload['index']
            assert i in (0, 1)
            self.send(self.workers[i], payload['command' if op == 'worker-call' else 'response'])
            return dict(worker=self.worker_read(i))
        if op == 'workers-stop':
            for proc in self.workers:
                self.send(proc, dict(op='stop', payload=None))
            for i, proc in enumerate(self.workers):
                assert self.worker_read(i)['op'] == 'stopped'
                assert proc.wait(timeout=5) == 0
            return dict(event='workers_stopped')
        if op == 'cycle-completed':
            assert payload['cycle'] == len(self.completed)+1
            self.receive_events()
            gets = self.events('get')
            accepts = [x for x in self.events('accept') if x['port'] == 443]
            assert len(gets) == len(accepts) == 1
            assert accepts[0]['peer'] in (self.active.probe4, self.active.probe6)
            assert gets[0]['target'] == accepts[0]['target']
            payload['receiver_get'] = gets[0]
            payload['receiver_accept'] = accepts[0]
            payload['receiver_get_count'] = len(gets)
            payload['physical_lineage'] = self.active.identity
            self.completed.append(payload)
            self.record('cycles', self.completed)
            self.record('worker-protocol', self.worker_log)
            print('cycle %s/3 completed: Run=%s next_due=%s' % (payload['cycle'], payload['run']['run_id'], payload['schedule']['next_due_at']), flush=True)
            self.archive_cycle()
            self.event_offset = len(self.receiver_events)
            self.cycle += 1
            self.life = None
            self.request_seen = False
            return dict(event='cycle_recorded')
        return super().pg_operation(instance, op, payload, selected, mode)

    def pg_prepare(self, instance, payload):
        # Fresh per-Run namespace; keep old evidence and never reuse its permit.
        self.active = self.instance(self.cycle, payload)
        self.record('cycle'+str(self.cycle+1)+'-lineage', payload)
        return dict(ready=self.active.boot())

    def driver_log_line(self, line):
        if line.startswith(b'NETWORK64 '):
            value = json.loads(line[len(b'NETWORK64 '):])
            if value['op'].startswith('worker'):
                value['payload'] = dict(redacted=True, bytes=len(line), sha256=hashlib.sha256(line).hexdigest())
                return b'PERIODIC-REDACTED '+json.dumps(value).encode()+b'\n'
        return super().driver_log_line(line)

    def pg(self, binary, mode):
        self.pg_binary = binary
        self.setup_control()
        driver_cmd = self.control_command(self.prefix+'-coordinator', binary.name, 'TestPeriodicSchedulerPG|TestPeriodicMigrationRoundTripPG|TestBudgetCleanupMigrationLifecycle', source=True)
        driver = subprocess.Popen(driver_cmd, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        self.processes.append(driver)
        log = []
        status = None
        try:
            until = time.monotonic()+300
            while driver.poll() is None or select.select([driver.stdout], [], [], 0)[0]:
                try:
                    line = self.read_bytes(driver, until)
                except RuntimeError:
                    if driver.poll() is not None:
                        break
                    raise
                log.append(self.driver_log_line(line))
                if not line.startswith(b'NETWORK64 '):
                    continue
                req = json.loads(line[len(b'NETWORK64 '):])
                op, payload = req['op'], req['payload']
                if op == 'catalog':
                    answer = self.public_catalog('93.184.216.34' if mode=='normal' else '2001:4860:ffff::34')
                elif op == 'prepare':
                    answer = self.pg_prepare(None, payload)
                else:
                    answer = self.pg_operation(None, op, payload, None, mode)
                self.send(driver, answer)
            status = driver.wait(timeout=5)
            assert status == 0 and len(self.completed) == 3, 'actual three-cycle PG18/Linux verification failed'
            assert len([x for x in self.receiver_events if x['kind']=='get']) == 3
            self.record('result', dict(task='9.1', exit_code=0, cycles=3, workers=2, real_interval_seconds=60,
                                      original_kernel=True, production_execution=False, storage_capacity_verified=False))
        finally:
            (self.dest/'pg-driver.log').write_bytes(b''.join(log))
            self.record('pg-driver-command', dict(command=driver_cmd, exit_code=status, binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest()))
            self.record('worker-protocol', self.worker_log)
            (self.dest/'postgres.log').write_bytes(subprocess.check_output(['docker', 'logs', cid], stderr=subprocess.STDOUT))
            sql = "SELECT jsonb_build_object('at',clock_timestamp(),'runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r),'schedules',(SELECT jsonb_agg(to_jsonb(s)) FROM monitor_schedules s),'reservations',(SELECT jsonb_agg(to_jsonb(q)) FROM scheduler_reservations q))"
            r = subprocess.run(['docker', 'exec', cid, 'psql', '-U', 'devpilot_fixture', '-d', 'devpilot_registration_test', '-At', '-c', sql], capture_output=True)
            (self.dest/'postgres-final-ledger.log').write_bytes(r.stdout+r.stderr)

    def setup_control(self):
        self.control = self.prefix+'-control'
        self.run(['docker', 'network', 'create', '--internal', '--ipv6', '--label', LABEL+'='+self.prefix, self.control])
        self.networks.append(self.control)
        network = json.loads(self.run(['docker', 'network', 'inspect', self.control]))[0]
        assert network['Internal'] and network['EnableIPv6'] and not network['Ingress']
        self.record('control-network', network)
        cmd = ['docker', 'run', '-d', '--name', self.prefix+'-pg', '--label', LABEL+'='+self.prefix,
               '--network', self.control, '--network-alias', 'pg', '--cap-drop', 'ALL',
               '--cap-add', 'CHOWN', '--cap-add', 'DAC_OVERRIDE', '--cap-add', 'FOWNER', '--cap-add', 'SETUID', '--cap-add', 'SETGID',
               '--security-opt', 'no-new-privileges:true', '--memory=512m', '--cpus=1', '--pids-limit=128',
               '-e', 'POSTGRES_USER=devpilot_fixture', '-e', 'POSTGRES_PASSWORD='+self.pg_password,
               '-e', 'POSTGRES_DB=devpilot_registration_test', PG, 'postgres', '-c', 'cluster_name=devpilot-budget-disposable',
               '-c', 'log_lock_waits=on', '-c', 'deadlock_timeout=100ms']
        cid = self.run(cmd).decode().strip()
        self.pg_id = cid
        self.control_ids.add(cid)
        self.containers.append(cid)
        # PG entrypoint needs only the existing filesystem/UID startup caps.
        # It has no NET_ADMIN, host path, host port or data-plane connection.
        self.record('postgres-container', json.loads(self.run(['docker', 'inspect', cid]))[0])
        until = time.monotonic()+30
        while subprocess.run(['docker', 'exec', cid, 'pg_isready', '-U', 'devpilot_fixture', '-d', 'devpilot_registration_test'], capture_output=True).returncode:
            if time.monotonic()>until:
                raise RuntimeError('fresh PG18 unavailable after30s')
            time.sleep(.2)
        self.setup_receiver()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--evidence', required=True, type=Path)
    parser.add_argument('--tools', required=True, type=Path)
    parser.add_argument('--mode', choices=['normal', 'race'], required=True)
    args = parser.parse_args()
    h = PeriodicHarness(args.evidence.resolve(), args.tools.resolve(), args.mode)
    status = 1
    try:
        h.pg(h.tools/('periodic-pg.test' if args.mode=='normal' else 'periodic-pg-race.test'), args.mode)
        status = 0
    finally:
        h.record('runner-exit', dict(exit_code=status, task='9.1', production_execution=False))
        # --rm Workers disappear after success; the original teardown only
        # accepts still-existing owned containers. Their inspections are saved.
        h.containers = [cid for cid in h.containers if subprocess.run(['docker', 'inspect', cid], capture_output=True).returncode==0]
        h.control_ids = set()
        h.close()
    return status


if __name__ == '__main__':
    raise SystemExit(main())
