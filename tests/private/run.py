#!/usr/bin/env python3
"""Build and test the actual private management stack in a NEW disposable PG18.
No caller database URL, existing volume, public HTTP target, or execution worker.
"""
import argparse
import ipaddress
import json
import os
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
NODE = 'node:24-alpine@sha256:a34ab6d7f6f4813f4d0c5469d74685604b962fbdbf5793a7bc7b4998ecb867c8'


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--require-host-ingress', action='store_true', help='required on native Linux CI; Desktop is not ingress proof')
    p.add_argument('--logs', type=Path, required=True)
    args = p.parse_args()
    args.logs.mkdir(parents=True, exist_ok=False)
    project = 'devpilot-private-test-'+secrets.token_hex(4)
    denied = project+'-denied'
    env = {**os.environ, 'PRIVATE_SOURCE_DIR': str(ROOT)}
    for key in ('DATABASE_URL', 'TEST_DATABASE_URL'):
        env.pop(key, None)
    with tempfile.TemporaryDirectory(prefix=project) as temp, (args.logs/'commands.log').open('w') as log:
        work = Path(temp)
        shutil.copy(ROOT/'deploy/private/compose.yaml', work/'compose.yaml')
        shutil.copy(ROOT/'deploy/private/provision.sql', work/'provision.sql')
        def run(command, input=None, capture=False, timeout=900):
            # Commands contain paths/config only, never passwords or cookies.
            log.write(' '.join(str(s) for s in command)+'\n'); log.flush()
            r = subprocess.run(command, input=input, text=True, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=timeout)
            if not capture:
                log.write(r.stdout); log.flush()
            if r.returncode:
                raise RuntimeError('Command failed; see commands.log (credentials excluded)')
            return r.stdout
        def compose(*a, **kw):
            return run(['docker', 'compose', '--project-name', project, '--env-file', str(work/'.env'), '-f', str(work/'compose.yaml'), *a], **kw)
        with socket.socket() as s:
            s.bind(('127.0.0.1', 0)); port = s.getsockname()[1]
        def configure(cidr):
            (work/'.env').write_text(f'PRIVATE_BIND_IP=127.0.0.1\nPRIVATE_HTTPS_PORT={port}\nPRIVATE_HOST=private.devpilot.test\nPRIVATE_ALLOWED_CIDRS={cidr}\n')
            run(['python3', str(ROOT/'deploy/private/configure.py'), '--env', str(work/'.env'), '--output', str(work/'.runtime/nginx.conf')])
        configure('127.0.0.0/8')
        run(['python3', str(ROOT/'deploy/private/configure.py'), '--env', str(work/'.env'), '--output', str(work/'.runtime/nginx.conf'), '--init-secrets', str(work/'.secrets')])
        # Test CA is used only by this test client; production takes supplied TLS.
        run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1', '-keyout', str(work/'.secrets/tls.key'), '-out', str(work/'.secrets/tls.crt'), '-subj', '/CN=private.devpilot.test', '-addext', 'subjectAltName=DNS:private.devpilot.test'])
        for f in (work/'.secrets').iterdir():
            f.chmod(0o444)
        compose('config', '--quiet')
        # Dependency retrieval happens before starting the closed runtime.
        run(['docker', 'pull', NODE])
        compose('build', 'api', 'web', 'migrate')
        try:
            compose('up', '-d', '--wait', 'postgres')
            compose('run', '--rm', 'migrate', 'migrate')
            compose('run', '--rm', 'migrate', 'provision')
            compose('run', '--rm', 'migrate', 'check')
            sql = """DO $$ DECLARE q text; BEGIN
 IF session_user<>'devpilot_private_app' OR current_user<>session_user
 OR (SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=session_user)
 OR NOT pg_has_role(session_user,'devpilot_scheduler_api','MEMBER') THEN RAISE EXCEPTION 'wrong runtime identity'; END IF;
 FOREACH q IN ARRAY ARRAY[
 'CREATE TABLE public.forbidden(id int)', 'CREATE TEMP TABLE forbidden(id int)',
 'SELECT * FROM public.monitor_runs', 'SELECT * FROM public.scheduler_control',
 'UPDATE public.scheduler_control SET gate=''fixture''',
 'UPDATE public.users SET status=''disabled'' WHERE false',
 'SET ROLE devpilot_scheduler_owner', 'SET ROLE devpilot_scheduler_worker',
 'SET ROLE devpilot_scheduler_controller', 'SET ROLE devpilot_scheduler_supervisor',
 'GRANT devpilot_scheduler_worker TO devpilot_private_app'
 ] LOOP
   BEGIN EXECUTE q; RAISE EXCEPTION 'unexpected runtime privilege';
   EXCEPTION WHEN insufficient_privilege THEN NULL; END;
 END LOOP;
END $$;"""
            compose('run', '--rm', '--entrypoint', 'sh', 'migrate', '-c',
                    'PGPASSWORD=$(cat /run/secrets/app_password); export PGPASSWORD; exec psql -h postgres -U devpilot_private_app -d devpilot_private -X -v ON_ERROR_STOP=1', input=sql)
            compose('up', '-d', 'api')
            api = compose('ps', '-q', 'api', capture=True).strip()
            info = json.loads(run(['docker', 'inspect', api], capture=True))[0]
            edge = next(n for n in info['NetworkSettings']['Networks'] if n.endswith('_ingress'))
            network = json.loads(run(['docker', 'network', 'inspect', edge], capture=True))[0]
            assert network['Internal'], 'ingress must be an internal network'
            cidr = network['IPAM']['Config'][0]['Subnet']
            configure(cidr)
            compose('up', '-d', 'web')
            web = compose('ps', '-q', 'web', capture=True).strip()
            web_info = json.loads(run(['docker', 'inspect', web], capture=True))[0]
            ip = web_info['NetworkSettings']['Networks'][edge]['IPAddress']
            assert ip, 'private ingress must be running'
            # Host publication enters the proxy's separate ingress attachment.
            # These temporary Docker/loopback peers stand in for invited VPN
            # clients; production uses only the operator's actual VPN CIDRs.
            entry = next(n for n in web_info['NetworkSettings']['Networks'] if n.endswith('_entry'))
            entry_net = json.loads(run(['docker', 'network', 'inspect', entry], capture=True))[0]
            assert not entry_net['Internal']
            configure(cidr+','+entry_net['IPAM']['Config'][0]['Subnet']+',127.0.0.0/8')
            compose('restart', 'web')
            def client(mode, address=ip, net=edge):
                payload = json.dumps(dict(mode=mode, ip=address, port=port, email=secrets.token_hex(5)+'@example.com', password=secrets.token_hex(20)))
                output = run(['docker', 'run', '--rm', '-i', '--network', net, '--read-only', '--cap-drop=ALL', '--security-opt=no-new-privileges', '--user=65534:65534', '-v', str(ROOT/'tests/private/client.mjs')+':/client.mjs:ro', '-v', str(work/'.secrets/tls.crt')+':/ca.crt:ro', NODE, 'node', '/client.mjs'], input=payload)
                print(output.strip(), flush=True)
            # Bounded readiness, without replaying any business write.
            ready = False
            for _ in range(30):
                state = compose('logs', '--no-color', 'api', capture=True)
                if 'server started' in state or 'server listening' in state:
                    ready = True; break
                # The API's startup logger wording is not a contract. Its process
                # must remain running; real API requests below prove DB readiness.
                if compose('ps', '--status', 'running', '-q', 'api', capture=True).strip():
                    time.sleep(1); ready = True; break
                time.sleep(.2)
            assert ready, 'API did not start'
            client('functional')
            if args.require_host_ingress:
                code = run(['curl', '--noproxy', '*', '--silent', '--show-error', '--max-time', '10', '--resolve', f'private.devpilot.test:{port}:127.0.0.1', '--cacert', str(work/'.secrets/tls.crt'), '-o', '/dev/null', '-w', '%{http_code}', f'https://private.devpilot.test:{port}/api/v1/auth/me'], capture=True)
                assert code == '401', 'native Linux published HTTPS ingress is required'
                print('PASS: native Linux loopback-bound published HTTPS ingress', flush=True)
            # Separate internal peer network, not an upstream or public client.
            existing = [ipaddress.ip_network(c['Subnet']) for c in network['IPAM']['Config'] if c.get('Subnet')]
            candidates = [ipaddress.ip_network(f'192.168.{n}.0/24') for n in range(241, 255)]
            other = next(n for n in candidates if not any(n.overlaps(x) for x in existing if x.version == 4))
            run(['docker', 'network', 'create', '--internal', '--subnet', str(other), denied])
            run(['docker', 'network', 'connect', denied, web])
            denied_ip = json.loads(run(['docker', 'inspect', web], capture=True))[0]['NetworkSettings']['Networks'][denied]['IPAddress']
            client('denied', denied_ip, denied)
            compose('restart', 'web')
            client('rate')
            # Inspect fixed assertions only; never export business rows or hashes.
            summary = compose('exec', '-T', 'postgres', 'psql', '-U', 'devpilot_migrator', '-d', 'devpilot_private', '-At', '-v', 'ON_ERROR_STOP=1', '-c', "SELECT (SELECT count(*) FROM monitor_runs)=0 AND (SELECT gate='closed' AND emergency_stop FROM scheduler_control WHERE control_id='deployment')", capture=True).strip()
            assert summary == 't', 'no Run and no open execution gate are required'
            print('PASS: no Run/Probe; deployment gate remains closed', flush=True)
            (args.logs/'result.json').write_text(json.dumps({'passed': True, 'host_ingress_required': args.require_host_ingress, 'gate': 'closed', 'runs': 0})+'\n')
        finally:
            # Preserve this invocation's nonsecret service diagnostics, then
            # destroy only its new labelled database/network/volume resources.
            try:
                (args.logs/'services.log').write_text(compose('logs', '--no-color', capture=True))
            finally:
                compose('down', '-v', '--remove-orphans')
                subprocess.run(['docker', 'network', 'rm', denied], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    main()
