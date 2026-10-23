#!/usr/bin/env python3
"""Task6.4 trusted outer allocator. Docker API stays outside all test children.

Docker creates network-none namespaces. NET_ADMIN moves only fresh veth peers
into the owned namespaces; no Docker bridge, host interface or upstream exists.
Per Run, a NET_ADMIN management namespace and independent forward guard precede
any unprivileged child. No SYS_ADMIN/privileged/host network/socket mounts.
"""
import argparse
import base64
import datetime
import hashlib
import json
import os
from pathlib import Path
import select
import re
import subprocess
import sys
import time
import uuid


IMAGE = 'devpilot-scheduler-fixture'
HERE = Path(__file__).resolve().parent
ALIASES4 = ['93.184.216.35', '192.0.2.53', '169.254.169.254', '10.0.0.9', '11.0.0.9']
ALIASES6 = ['2001:4860:ffff::35', 'fd64:53::53', 'fd64:bad::254', '64:ff9b::5db8:d822', '2002:5db8:d822::1']
LABEL = 'devpilot.disposable.network64'


def now_string(delay=0):
    value = datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(seconds=delay)
    return value.isoformat(timespec='microseconds').replace('+00:00', 'Z')


def checked(cmd, *, data=None):
    r = subprocess.run(cmd, input=data, capture_output=True, timeout=20)
    if r.returncode:
        raise RuntimeError(f'command failed{r.returncode}: {cmd}: {r.stderr.decode()[:1000]}')
    return r.stdout


def lineage():
    result = {k: str(uuid.uuid4()) for k in ('workspace_id', 'project_id', 'monitor_id', 'run_id')}
    result.update({k: '1' for k in ('configuration_version', 'activation_generation', 'scheduler_epoch', 'fencing_token')})
    result.update({k: str(uuid.uuid4()) for k in ('namespace_incarnation', 'supervisor_incarnation')})
    return result


class Harness:
    def __init__(self, dest, binary):
        self.dest = dest
        self.binary = binary.resolve()
        self.prefix = 'devpilot-n64-' + uuid.uuid4().hex[:10]
        self.containers = []
        self.networks = []
        self.processes = []
        self.commands = []
        self.timeline = []
        self.cases = []
        self.instances = []
        dest.mkdir(parents=True, exist_ok=False)
        self.image_id = checked(['docker', 'image', 'inspect', IMAGE, '--format', '{{.Id}}']).decode().strip()

    def record(self, name, value):
        (self.dest/(name+'.json')).write_text(json.dumps(value, indent=2)+'\n')

    def run(self, cmd, data=None):
        started = now_string()
        try:
            out = checked(cmd, data=data)
        except Exception:
            self.commands.append(dict(command=cmd, started=started, failed=True))
            raise
        self.commands.append(dict(command=cmd, started=started, exit_code=0))
        return out

    def exec(self, container, *cmd, data=None):
        return self.run(['docker', 'exec', '-i', container, *cmd], data)

    def create(self, suffix, network="none", ip4=None, ip6=None, admin=True, pid=None):
        name = self.prefix+'-'+suffix
        cmd = ['docker', 'run', '-d', '--name', name, '--label', LABEL+'='+self.prefix,
               '--network', network, '--cap-drop', 'ALL', '--security-opt', 'no-new-privileges:true',
               '--read-only', '--memory=128m', '--cpus=1', '--pids-limit=128', '--ulimit', 'nofile=256:256',
               '--tmpfs', '/tmp:rw,noexec,nosuid,size=4m', '--tmpfs', '/var/lib/postgresql:ro,noexec,nosuid,size=4k']
        if admin:
            forwarding = '1' if suffix.endswith('-guard') else '0'
            cmd += ['--cap-add', 'NET_ADMIN', '--sysctl', 'net.ipv6.conf.all.disable_ipv6=0',
                    '--sysctl', 'net.ipv4.ip_forward='+forwarding, '--sysctl', 'net.ipv6.conf.all.forwarding='+forwarding,
                    '--sysctl', 'net.ipv6.conf.all.accept_ra=0', '--sysctl', 'net.ipv6.conf.default.accept_ra=0']
        else:
            cmd += ['--user', '65534:65534']
        if pid:
            cmd += ['--pid', 'container:'+pid]
        if ip4:
            cmd += ['--ip', ip4, '--ip6', ip6]
        if admin:
            cmd += ['-v', str(self.binary)+':/usr/local/bin/networksupervisorfixture:ro']
        cmd += [IMAGE, '-c', 'exec sleep infinity']
        cid = self.run(cmd).decode().strip()
        self.containers.append(cid)
        proof = json.loads(self.run(['docker', 'inspect', cid]))[0]
        assert not proof['HostConfig']['Privileged'] and not proof['HostConfig']['PortBindings']
        assert [x.removeprefix('CAP_') for x in (proof['HostConfig']['CapAdd'] or [])] == (['NET_ADMIN'] if admin else [])
        assert not any('docker.sock' in m.get('Source', '') or 'devpilot' in m.get('Source', '') and not m.get('Source', '').endswith('networksupervisorfixture') for m in proof['Mounts'])
        self.record(suffix+'-container', proof)
        return cid

    def assert_wires(self):
        proofs=[]
        for cid in self.containers:
            n=json.loads(self.run(['docker','inspect',cid]))[0]
            mode=n['HostConfig']['NetworkMode']
            assert mode=='none' or mode.startswith('container:') and mode.split(':',1)[1] in self.containers
            assert n['Config']['Labels'][LABEL]==self.prefix
            assert not n['HostConfig']['Privileged'] and not n['HostConfig']['PortBindings']
            proofs.append(dict(container=cid,network_mode=mode,pid_mode=n['HostConfig']['PidMode']))
        self.record('namespace-allocation',proofs)

    def setup_receiver(self):
        self.receiver = self.create('receiver')
        for ip in ['93.184.216.34']+ALIASES4:
            self.exec(self.receiver, 'ip', 'addr', 'add', ip+'/32', 'dev', 'lo')
        for ip in ['2001:4860:ffff::34']+ALIASES6:
            self.exec(self.receiver, 'ip', '-6', 'addr', 'add', ip+'/128', 'dev', 'lo', 'nodad')
        self.exec(self.receiver, 'ip', 'neigh', 'flush', 'all')
        proc = subprocess.Popen(['docker', 'exec', '-i', self.receiver, 'python3', '-u', '-'], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=open(self.dest/'receiver.stderr', 'wb'))
        proc.stdin.write(self.receiver_source())
        proc.stdin.close()
        self.processes.append(proc)
        self.receiver_proc = proc
        event = self.readline(proc, 10)
        assert event['kind'] == 'ready'
        self.certificate = event.pop('certificate')
        self.receiver_events = [event]
        self.receiver_fd = proc.stdout

    def readline(self, proc, timeout=3):
        # Use one byte at a time: select on an OS descriptor must not miss a
        # second JSON line already buffered inside a Python file object.
        line = bytearray()
        until = time.monotonic()+timeout
        while len(line) <= 8192:
            if not select.select([proc.stdout], [], [], max(0, until-time.monotonic()))[0]:
                raise RuntimeError('bounded management channel wait expired')
            b = os.read(proc.stdout.fileno(), 1)
            if not b:
                raise RuntimeError('management channel closed without confirmation')
            line += b
            if b == b'\n':
                return json.loads(line)
        raise RuntimeError('oversized management reply')

    def receive_events(self):
        while select.select([self.receiver_fd], [], [], .15)[0]:
            self.receiver_events.append(self.readline(self.receiver_proc))
        self.record('receiver-events', self.receiver_events)

    def events(self, kind='get'):
        self.receive_events()
        return [x for x in self.receiver_events if x['kind'] == kind]

    def instance(self, index, identity=None):
        instance=Instance(self,index,identity or lineage())
        self.instances.append(instance)
        return instance

    def close(self):
        # Destruction is only outer-fixture teardown, never a signed Supervisor
        # stop receipt and never a reason to release database quarantine slots.
        if hasattr(self,'receiver_events'):
            self.receive_events()
        for instance in self.instances:
            if instance.run in self.containers:
                try:
                    instance.snapshot('final')
                except Exception as e:
                    self.record(instance.name+'-snapshot-error',dict(error=str(e)))
        for p in self.processes:
            if p.poll() is None:
                p.terminate()
        for cid in reversed(self.containers):
            p = json.loads(checked(['docker', 'inspect', cid]))[0]
            assert p['Config']['Labels'].get(LABEL) == self.prefix
            subprocess.run(['docker', 'rm', '-f', '-v', cid], check=True, capture_output=True)
        for name in reversed(self.networks):
            n = json.loads(checked(['docker', 'network', 'inspect', name]))[0]
            assert n['Labels'].get(LABEL) == self.prefix
            subprocess.run(['docker', 'network', 'rm', name], check=True, capture_output=True)
        self.record('commands', self.commands)
        self.record('cases', self.cases)
        self.record('timeline', self.timeline)
        files = {str(p.relative_to(self.dest)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(self.dest.rglob('*')) if p.is_file() and p.name!='manifest.json'}
        self.record('manifest', dict(image_id=self.image_id, binary_sha256=hashlib.sha256(self.binary.read_bytes()).hexdigest(), files_sha256=files))

    def pg(self, binary, mode):
        """One fresh guarded cluster; held/quarantine rows are never faked free."""
        self.setup_receiver()
        instance=self.instance(0)
        selected='93.184.216.34' if mode=='normal' else '2001:4860:ffff::34'
        image='postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336'
        label=self.prefix+'-pg'
        cid=self.run(['docker','run','-d','--name',label,'--label',LABEL+'='+self.prefix,'--network','none',
                      '--memory=512m','--cpus=1','--pids-limit=128',
                      '-e','POSTGRES_USER=devpilot_fixture','-e','POSTGRES_PASSWORD=devpilot_fixture_only','-e','POSTGRES_DB=devpilot_registration_test',
                      image,'postgres','-c','cluster_name=devpilot-budget-disposable','-c','log_lock_waits=on','-c','deadlock_timeout=100ms']).decode().strip()
        self.containers.append(cid)
        until=time.monotonic()+30
        while subprocess.run(['docker','exec',cid,'pg_isready','-h','127.0.0.1','-U','devpilot_fixture','-d','devpilot_registration_test'],capture_output=True).returncode:
            if time.monotonic()>until:raise RuntimeError('fresh PG18 readiness exceeded30s')
            time.sleep(.2)
        repo=HERE.parents[1]
        cmd=['docker','run','--rm','-i','--network','container:'+cid,'--cap-drop','ALL','--security-opt','no-new-privileges:true',
             '-v',str(repo)+':'+str(repo)+':ro','-v',str(binary.parent)+':/toolbin:ro','-w',str(repo),
             '-e','PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin',
             '-e','TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable',
             '--entrypoint','/toolbin/'+binary.name,IMAGE,'-test.run',self.pg_test_name(),'-test.v','-test.timeout=120s']
        driver=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        self.processes.append(driver)
        log=[]
        try:
            while True:
                line=bytearray()
                end=time.monotonic()+120
                while True:
                    if not select.select([driver.stdout],[],[],max(0,end-time.monotonic()))[0]:raise RuntimeError('PG driver bounded120s wait expired')
                    b=os.read(driver.stdout.fileno(),1)
                    if not b:break
                    line+=b
                    if b==b'\n':break
                    if len(line)>1048576:raise RuntimeError('unbounded PG driver output')
                if not line:break
                log.append(self.driver_log_line(bytes(line)))
                if not line.startswith(b'NETWORK64 '):continue
                request=json.loads(line[len(b'NETWORK64 '):])
                op=request['op']
                if op=='catalog':answer=self.public_catalog(selected)
                elif op=='prepare':
                    instance.identity=request['payload']
                    answer=dict(ready=instance.boot())
                elif op=='install':
                    confirmation=instance.install(data=request['payload'])
                    answer=dict(confirmation=confirmation)
                elif op=='witness':
                    answer=self.pg_witness(instance,selected,mode)
                else:answer=self.pg_operation(instance,op,request['payload'],selected,mode)
                driver.stdin.write(json.dumps(answer).encode()+b'\n');driver.stdin.flush()
            status=driver.wait(timeout=3)
            assert status==0, 'actual PG/network driver failed'
            self.cases.append(self.pg_completion(mode,status))
        finally:
            (self.dest/'pg-driver.log').write_bytes(b''.join(log))
            self.record('pg-driver-command',dict(command=cmd,exit_code=driver.poll(),binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest()))
            (self.dest/'postgres.log').write_bytes(subprocess.check_output(['docker','logs',cid],stderr=subprocess.STDOUT))
            sql="SELECT jsonb_build_object('at',clock_timestamp(),'budgets',(SELECT jsonb_agg(to_jsonb(b)) FROM scheduler_budget_state b),'runs',(SELECT jsonb_agg(to_jsonb(r)) FROM monitor_runs r),'reservations',(SELECT jsonb_agg(to_jsonb(q)) FROM scheduler_reservations q),'locks',(SELECT jsonb_agg(to_jsonb(l)) FROM pg_locks l),'activity',(SELECT jsonb_agg(jsonb_build_object('pid',pid,'wait',wait_event,'blockers',pg_blocking_pids(pid))) FROM pg_stat_activity WHERE datname=current_database()))"
            result=subprocess.run(['docker','exec',cid,'psql','-U','devpilot_fixture','-d','devpilot_registration_test','-At','-c',sql],capture_output=True)
            (self.dest/'postgres-final-ledger.log').write_bytes(result.stdout+result.stderr)


    def receiver_source(self):
        return (HERE/'network_receiver.py').read_bytes()

    def driver_log_line(self,line):
        return line

    def pg_operation(self,instance,op,payload,selected,mode):
        raise RuntimeError('unknown driver operation')

    def pg_completion(self,mode,status):
        return dict(case='real-pg18-network-binding-'+mode,exit_code=status,selected_family=4 if mode=='normal' else 6,physical_ledger_not_released=True)

    def pg_test_name(self):
        return '^TestBudgetNetworkPolicyPG$'

    def public_catalog(self,selected):
        return dict(ip=selected)

    def pg_witness(self,instance,selected,mode):
        before=len(self.events())
        result=instance.actor([dict(ip=selected,port=443,tls=True)],'pg-'+mode+'-allowed')
        assert result['actions'][0]['success'] and len(self.events())==before+1
        assert self.events()[-1]['sni']=='a.fixture.example.com' and not self.events()[-1]['credentials']
        instance.snapshot('pg-witness')
        # Actual fixture destruction, NOT an application termination
        # receipt. DB ledgers remain reserved through evidence capture.
        for p in instance.proc.values():p.stdin.close();assert p.wait(timeout=3)==0
        for owned in (instance.actor_container,instance.gateway,instance.run):
            self.run(['docker','rm','-f','-v',owned]);self.containers.remove(owned)
        return dict(fixture_teardown_complete=True)


class Instance:
    def __init__(self, h, index, identity):
        self.h, self.index, self.identity = h, index, identity
        base = 64+index//16
        last = (index % 16)*16
        self.probe4 = f'198.19.{base}.{last+2}'
        self.gate4 = f'198.19.{base}.{last+3}'
        self.probe6 = f'fd64:64:{index+1:x}::2'
        self.gate6 = f'fd64:64:{index+1:x}::3'
        self.back4 = f'198.19.66.{index*4+1}'
        self.receiver4 = f'198.19.66.{index*4+2}'
        self.back6 = f'fd64:65:{index+1:x}::1'
        self.receiver6 = f'fd64:65:{index+1:x}::2'
        self.name = f'run-{index}'
        self.run = h.create(self.name)
        # Shares only the disposable management PID namespace in order to move
        # a newly-created peer through NETLINK (NET_ADMIN), without setns or
        # SYS_ADMIN. The unprivileged child has its own PID namespace.
        self.gateway = h.create(self.name+'-guard',pid=self.run)
        h.exec(self.gateway,'ip','link','add','eth0','type','veth','peer','name','runpeer')
        h.exec(self.gateway,'ip','link','set','runpeer','netns','1')
        h.exec(self.run,'ip','link','set','runpeer','name','eth0')
        rx='rx'+str(index)
        helper=['docker','run','--rm','--network','container:'+self.gateway,'--pid','container:'+h.receiver,
                '--cap-drop','ALL','--cap-add','NET_ADMIN','--security-opt','no-new-privileges:true','--read-only',IMAGE,'-c',
                f'ip link add eth1 type veth peer name {rx} && ip link set {rx} netns 1']
        h.run(helper)
        for cid,dev,ip4,ip6,mask4 in ((self.run,'eth0',self.probe4,self.probe6,28),
                                    (self.gateway,'eth0',self.gate4,self.gate6,28),
                                    (self.gateway,'eth1',self.back4,self.back6,30),
                                    (h.receiver,rx,self.receiver4,self.receiver6,30)):
            h.exec(cid,'ip','addr','add',ip4+'/'+str(mask4),'dev',dev)
            h.exec(cid,'ip','link','set','dev',dev,'addrgenmode','none')
            h.exec(cid,'ip','-6','addr','add',ip6+'/64','dev',dev,'nodad')
            h.exec(cid,'ip','link','set',dev,'up')
        self.proc = {}
        self.config = {}
        self.ready = {}
        self.actor_container = None
        self.run_mac = json.loads(h.exec(self.run, 'ip', '-j', 'link', 'show', 'eth0'))[0]['address']
        self.gate_mac = json.loads(h.exec(self.gateway, 'ip', '-j', 'link', 'show', 'eth0'))[0]['address']
        self.back_mac = json.loads(h.exec(self.gateway, 'ip', '-j', 'link', 'show', 'eth1'))[0]['address']
        receiver_mac = json.loads(h.exec(h.receiver, 'ip', '-j', 'link', 'show', rx))[0]['address']
        for family,ip,dev,mac,cid in [('-4',self.receiver4,'eth1',receiver_mac,self.gateway),
                                    ('-6',self.receiver6,'eth1',receiver_mac,self.gateway),
                                    ('-4',self.back4,rx,self.back_mac,h.receiver),
                                    ('-6',self.back6,rx,self.back_mac,h.receiver)]:
            h.exec(cid,'ip',family,'neigh','replace',ip,'lladdr',mac,'nud','permanent','dev',dev)
        for cid, ip, ip6, mac in ((self.run, self.gate4, self.gate6, self.gate_mac), (self.gateway, self.probe4, self.probe6, self.run_mac)):
            h.exec(cid, 'ip', 'neigh', 'replace', ip, 'lladdr', mac, 'nud', 'permanent', 'dev', 'eth0')
            h.exec(cid, 'ip', '-6', 'neigh', 'replace', ip6, 'lladdr', mac, 'nud', 'permanent', 'dev', 'eth0')
        for ip, family, mask in [(x, '-4', '/32') for x in ['93.184.216.34']+ALIASES4]+[(x, '-6', '/128') for x in ['2001:4860:ffff::34']+ALIASES6]:
            h.exec(self.run, 'ip', family, 'route', 'replace', ip+mask, 'via', self.gate4 if family=='-4' else self.gate6, 'dev', 'eth0')
            h.exec(self.gateway, 'ip', family, 'route', 'replace', ip+mask, 'via', self.receiver4 if family=='-4' else self.receiver6, 'dev', 'eth1')
        for ip, ip6 in ((self.probe4, self.probe6),):
            h.exec(h.receiver, 'ip', 'route', 'replace', ip+'/32', 'via', self.back4, 'dev', rx)
            h.exec(h.receiver, 'ip', '-6', 'route', 'replace', ip6+'/128', 'via', self.back6, 'dev', rx)
            h.exec(h.receiver, 'ip', 'neigh', 'replace', self.back4, 'lladdr', self.back_mac, 'nud', 'permanent', 'dev', rx)
            h.exec(h.receiver, 'ip', '-6', 'neigh', 'replace', self.back6, 'lladdr', self.back_mac, 'nud', 'permanent', 'dev', rx)
        h.assert_wires()

    def boot(self, deadline=None):
        until = deadline or now_string(34)
        for role, cid in (('gateway', self.gateway), ('run', self.run)):
            fp = json.loads(self.h.exec(cid, '/usr/local/bin/networksupervisorfixture', 'fingerprint'))
            cfg = dict(lineage=self.identity, role=role, **fp, probe4=self.probe4, probe6=self.probe6, host='a.fixture.example.com', hard_deadline=until)
            proc = subprocess.Popen(['docker', 'exec', '-i', cid, '/usr/local/bin/networksupervisorfixture'], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                    stderr=open(self.h.dest/(self.name+'-'+role+'.stderr'), 'wb'))
            self.h.processes.append(proc)
            self.proc[role], self.config[role] = proc, cfg
            proc.stdin.write(json.dumps(dict(op='boot', config=cfg)).encode()+b'\n'); proc.stdin.flush()
            reply = self.h.readline(proc)
            assert reply['namespace']==cfg['namespace'] and reply['role']==role
            self.ready[role] = reply
        assert self.ready['run']['namespace'] != self.ready['gateway']['namespace']
        self.h.record(self.name+'-ready', self.ready)
        self.h.timeline.append(dict(event='both_rules_verified_before_child', run=self.identity['run_id'], at=now_string(), monotonic_ns=time.monotonic_ns()))
        self.snapshot('ready')
        for role,cid in [('run',self.run),('gateway',self.gateway)]:
            for group in ('resolver4','resolver6'):
                self.expiry_bound(role,cid,group,until)
        return self.ready

    def expiry_bound(self,role,cid,group,deadline_string):
        text=self.h.exec(cid,'nft','list','set','inet','dp_run',group).decode()
        observed=int(self.h.exec(cid,'python3','-c','import time; print(time.time_ns())'))/1e9
        expires=re.findall(r'expires ([0-9a-z]+)',text)
        assert len(expires)==1,'exactly one live expiring element required'
        parts=re.findall(r'(\d+)(ms|s|m|h|d)',expires[0])
        assert ''.join(n+unit for n,unit in parts)==expires[0]
        seconds=sum(int(n)*{'ms':.001,'s':1,'m':60,'h':3600,'d':86400}[unit] for n,unit in parts)
        deadline=datetime.datetime.fromisoformat(deadline_string.replace('Z','+00:00')).timestamp()
        assert observed+seconds+.001<=deadline, 'kernel permit extends fixed deadline'
        self.h.record(self.name+'-'+role+'-'+group+'-expiry-bound',dict(kernel_rules=text,observed_linux_utc=observed,expires_seconds=seconds,conservative_expiry_upper=observed+seconds+.001,request_deadline=deadline))

    def install(self, data=None, ip='93.184.216.34', port=80, seconds=3):
        request = dict(**self.identity, host='a.fixture.example.com', scheme='https' if port==443 else 'http', ip=ip, port=port, deadline=now_string(seconds))
        # Standalone policy cases use a synthetic proof, explicitly separate
        # from the real PG driver. Its timestamp must come from the Linux clock,
        # not Darwin's independent wall clock. Never relax the future check.
        committed=self.h.exec(self.gateway,'python3','-c','import datetime; print(datetime.datetime.now(datetime.timezone.utc).isoformat())').decode().strip()
        x = data or dict(request=request, permit_id=str(uuid.uuid4()), committed_at=committed, permit_deadline=now_string(30))
        self.h.record(self.name+'-install-request',x)
        replies=[]
        for role in ('gateway', 'run'):
            p=self.proc[role]
            p.stdin.write(json.dumps(dict(op='install', install=x)).encode()+b'\n');p.stdin.flush()
            reply=self.h.readline(p)
            self.h.record(self.name+'-'+role+'-confirmation',reply)
            expected=x['request'].copy();actual=reply['request'].copy()
            parse=lambda s:datetime.datetime.fromisoformat(s.replace('Z','+00:00'))
            assert parse(actual.pop('deadline'))==parse(expected.pop('deadline'))
            assert actual==expected and reply['permit_id']==x['permit_id'] and parse(reply['deadline'])==parse(x['request']['deadline'])
            replies.append(reply)
        self.h.record(self.name+'-installed', dict(install=x, confirmations=replies, at=now_string()))
        self.snapshot('installed')
        # nft JSON rounds timeouts to seconds. Use its millisecond text and a
        # subsequent Linux-clock sample to derive a conservative expiry bound.
        for role,cid in [('run',self.run),('gateway',self.gateway)]:
            self.expiry_bound(role,cid,'target6' if ':' in x['request']['ip'] else 'target4',x['request']['deadline'])
        return replies[-1]

    def snapshot(self, phase):
        for role, cid in (('run', self.run), ('gateway', self.gateway)):
            data={}
            for key,args in [('rules',['nft','-j','list','ruleset']),('links',['ip','-j','-d','link','show']),('addresses',['ip','-j','addr','show']),('route4',['ip','-4','-j','route','show','table','all']),('route6',['ip','-6','-j','route','show','table','all']),('policy4',['ip','-4','-j','rule','show']),('policy6',['ip','-6','-j','rule','show']),('neighbors',['ip','-j','neigh','show'])]:
                data[key]=json.loads(self.h.exec(cid,*args))
            data['namespace']=self.h.exec(cid,'readlink','/proc/self/ns/net').decode().strip()
            data['iptables']=self.h.exec(cid,'iptables-save').decode()
            data['ip6tables']=self.h.exec(cid,'ip6tables-save').decode()
            self.h.record(self.name+'-'+role+'-'+phase,data)

    def actor(self, actions, label, admin=False):
        assert self.ready or label.startswith('malicious-'), 'legitimate child before verified policy'
        if self.actor_container is None:
            self.actor_container=self.h.create(self.name+'-child','container:'+self.run,admin=False)
            ns=self.h.exec(self.actor_container,'readlink','/proc/self/ns/net').decode().strip()
            assert ns==self.h.exec(self.run,'readlink','/proc/self/ns/net').decode().strip()
        config=dict(actions=actions,admin=admin,gateway=self.gate4,certificate=self.h.certificate)
        script=(HERE/'network_actor.py').read_text()+'\nprint(json.dumps(run('+repr(config)+')),flush=True)\n'
        cmd=['docker','exec','-i',self.actor_container,'strace','-f','-e','trace=network,process','env','-i','LANG=C','LC_ALL=C','TZ=UTC','python3','-']
        self.h.timeline.append(dict(event='unprivileged_child',label=label,at=now_string(),monotonic_ns=time.monotonic_ns()))
        r=subprocess.run(cmd,input=script.encode(),capture_output=True,timeout=15)
        (self.h.dest/(self.name+'-'+label+'.trace')).write_bytes(r.stderr)
        self.h.record(self.name+'-'+label+'-command',dict(command=cmd,exit_code=r.returncode))
        if r.returncode:
            raise RuntimeError('unprivileged child failed: '+r.stderr.decode()[-2000:]+r.stdout.decode())
        result=json.loads(r.stdout)
        self.h.record(self.name+'-'+label,result)
        return result

    def reject_install(self, mutate, label, role='run'):
        r=dict(**self.identity,host='a.fixture.example.com',scheme='http',ip='93.184.216.34',port=80,deadline=now_string(5))
        committed=self.h.exec(self.gateway,'python3','-c','import datetime; print(datetime.datetime.now(datetime.timezone.utc).isoformat())').decode().strip()
        x=dict(request=r,permit_id=str(uuid.uuid4()),committed_at=committed,permit_deadline=now_string(25))
        mutate(x)
        p=self.proc[role];p.stdin.write(json.dumps(dict(op='install',install=x)).encode()+b'\n');p.stdin.flush()
        try:
            self.h.readline(p)
        except RuntimeError as e:
            assert 'closed without confirmation' in str(e), str(e)
        else:
            raise AssertionError('unsafe install confirmed')
        assert p.wait(timeout=3)==2
        expected={'same-run-second-permit':'fixture_network_consumed','rule-loss-rejects-install':'fixture_network_rules',
                  'gateway-table-loss-rejects-install':'fixture_network_rules','unexpected-default-route':'fixture_network_topology',
                  'policy-route-drift':'fixture_network_topology','expiry-drift':'fixture_network_rules','future-commit-proof':'fixture_network_future_commit_proof'}.get(label,'fixture_network_binding')
        reason=(self.h.dest/(self.name+'-'+role+'.stderr')).read_text().strip()
        assert reason==expected,(label,reason,expected)
        self.h.cases.append(dict(case=label,rejected=True,stage=reason))


def suite(h):
    h.setup_receiver()
    a=h.instance(0);b=h.instance(1)
    a.boot();b.boot()
    assert a.ready['run']['namespace']!=b.ready['run']['namespace']
    dns=[dict(ip='192.0.2.53',port=53,dns=True),dict(ip='fd64:53::53',port=53,dns=True)]
    result=a.actor(dns,'exact-resolver',True)
    assert all(x['success'] for x in result['actions'])
    assert len(h.events('dns'))==2
    forbidden=[dict(ip=x,port=80) for x in ALIASES4+ALIASES6]+[dict(ip='93.184.216.34',port=p) for p in (53,80,443,8080)]
    forbidden += [dict(ip='::ffff:169.254.169.254',port=80),dict(ip=a.gate4,port=80),dict(ip=a.gate6,port=80),dict(ip='127.0.0.11',port=53)]
    forbidden += [dict(ip=x,port=53,udp=True) for x in ('192.0.2.53','fd64:53::53')]
    before=len(h.events('accept'))
    before_udp=len(h.events('udp'))
    result=a.actor(forbidden,'malicious-before-permit')
    assert not any(x['connected'] for x in result['actions']) and len(h.events('accept'))==before
    assert len(h.events('udp'))==before_udp
    h.cases.append(dict(case='dual-stack-default-deny',attempts=len(forbidden),receiver_accepts=0))
    a.install(seconds=8)
    result=a.actor([dict(ip='93.184.216.34',port=80)],'allowed-ipv4')
    assert result['actions'][0]['success'] and len(h.events())==1
    before=len(h.events('accept'))
    again=a.actor([dict(ip='93.184.216.34',port=80)],'malicious-second-connection')
    assert not again['actions'][0]['connected'] and len(h.events('accept'))==before
    h.cases.append(dict(case='one-physical-target-connection',second_receiver_accepts=0))
    before=len(h.events('accept'))
    result=b.actor([dict(ip='93.184.216.34',port=80)],'malicious-cross-run')
    assert not result['actions'][0]['connected'] and len(h.events('accept'))==before
    h.cases.append(dict(case='per-run-veth-and-permit-isolation',distinct_namespaces=True))
    # Local rules removed by the trusted failure injector; the compromised
    # no-capability child still cannot bypass the independent forward guard.
    h.exec(a.run,'nft','delete','table','inet','dp_run')
    before=len(h.events('accept'))
    result=a.actor(forbidden,'malicious-local-rules-missing')
    # The authorized exact endpoint can still be allowed until its fixed expiry;
    # no other endpoint may reach the receiver even with the local policy gone.
    assert all(not x['connected'] for x in result['actions'] if not(x['ip']=='93.184.216.34' and x['port']==80))
    received=h.events('accept')[before:]
    assert all(x['target']=='93.184.216.34' and x['port']==80 for x in received)
    h.cases.append(dict(case='independent-guard-after-local-table-loss',forbidden_accepts=0))
    c=h.instance(2);c.boot();c.install(ip='2001:4860:ffff::34',port=443,seconds=5)
    result=c.actor([dict(ip='2001:4860:ffff::34',port=443,tls=True)],'allowed-ipv6-tls')
    assert result['actions'][0]['success']
    get=h.events()[-1];assert get['sni']=='a.fixture.example.com' and not get['credentials']
    # Kill only the management process/channel. No kernel renewal is possible;
    # expiry is tested independently, without claiming physical process stop.
    for p in c.proc.values():p.stdin.close();assert p.wait(timeout=3)==0
    time.sleep(5.1)
    before=len(h.events('accept'))
    result=c.actor([dict(ip='2001:4860:ffff::34',port=443,tls=True)],'supervisor-loss-expired')
    assert not result['actions'][0]['connected'] and len(h.events('accept'))==before
    h.cases.append(dict(case='supervisor-loss-kernel-expiry',receiver_accepts_after_expiry=0))
    for i,(label,mutate) in enumerate([
        ('wrong-run',lambda x:x['request'].update(run_id=str(uuid.uuid4()))),
        ('wrong-scope',lambda x:x['request'].update(workspace_id=str(uuid.uuid4()))),
        ('wrong-fence',lambda x:x['request'].update(fencing_token='2')),
        ('wrong-host',lambda x:x['request'].update(host='other.fixture.example.com')),
        ('wrong-ip',lambda x:x['request'].update(ip='169.254.169.254')),
        ('expired-request',lambda x:x['request'].update(deadline=now_string(-1))),
        ('extend-hard-cutoff',lambda x:x['request'].update(deadline=now_string(60))),
        ('uncommitted-proof',lambda x:x.update(committed_at='0001-01-01T00:00:00Z')),
    ],3):
        instance=h.instance(i);instance.boot();instance.reject_install(mutate,label)
        before=len(h.events('accept'))
        result=instance.actor([dict(ip='93.184.216.34',port=80)],'malicious-'+label)
        assert not result['actions'][0]['connected'] and len(h.events('accept'))==before
    d=h.instance(11);d.boot();h.exec(d.run,'nft','delete','table','inet','dp_run')
    d.reject_install(lambda x:None,'rule-loss-rejects-install')
    before=len(h.events('accept'))
    assert not d.actor([dict(ip='93.184.216.34',port=80)],'malicious-rule-loss')['actions'][0]['connected']
    assert len(h.events('accept'))==before
    e=h.instance(12);e.boot();e.install(seconds=5)
    e.reject_install(lambda x:None,'same-run-second-permit')
    # Restart cannot erase the private bootstrap ledger, even after the rule
    # table is lost. This is not a proof of cross-node recovery (task6.5).
    h.exec(e.run,'nft','delete','table','inet','dp_run')
    cfg=e.config['run'].copy();cfg['hard_deadline']=now_string(34)
    restarted=subprocess.run(['docker','exec','-i',e.run,'/usr/local/bin/networksupervisorfixture'],input=json.dumps(dict(op='boot',config=cfg)).encode()+b'\n',capture_output=True,timeout=3)
    assert restarted.returncode==2 and not restarted.stdout
    h.record('restart-same-namespace',dict(exit_code=restarted.returncode,stderr=restarted.stderr.decode(),no_confirmation=True))
    h.cases.append(dict(case='restart-and-table-loss-cannot-renew',rejected=True))
    f=h.instance(13);f.boot();h.exec(f.gateway,'nft','delete','table','inet','dp_run')
    f.reject_install(lambda x:None,'gateway-table-loss-rejects-install','gateway')
    before=len(h.events('accept'));assert not f.actor([dict(ip='93.184.216.34',port=80)],'malicious-gateway-loss')['actions'][0]['connected'];assert len(h.events('accept'))==before
    g=h.instance(14);g.boot();h.exec(g.run,'ip','route','add','default','via',g.gate4)
    g.reject_install(lambda x:None,'unexpected-default-route')
    q=h.instance(15);q.boot()
    for p in q.proc.values():p.stdin.close();assert p.wait(timeout=3)==0
    before=len(h.events('accept'));assert not q.actor([dict(ip='93.184.216.34',port=80)],'malicious-supervisor-loss-before-install')['actions'][0]['connected'];assert len(h.events('accept'))==before
    h.cases.append(dict(case='supervisor-loss-before-install',receiver_accepts=0))
    # Actual post-routing transforms: the independent forward policy sees the
    # translated destination and blocks it, with no broad ESTABLISHED accept.
    for index,ip,dest,family in [(16,'93.184.216.34','169.254.169.254','ip'),(17,'2001:4860:ffff::34','fd64:bad::254','ip6')]:
        n=h.instance(index);n.boot();n.install(ip=ip,seconds=5)
        script=(f'table {family} bad_translation {{\n chain pre {{\n'
                '  type nat hook prerouting priority dstnat; policy accept;\n'
                f'  {family} daddr {ip} tcp dport 80 dnat to {dest};\n }}\n}}\n')
        h.exec(n.gateway,'nft','-f','-',data=script.encode())
        before=len(h.events('accept'));result=n.actor([dict(ip=ip,port=80)],'malicious-dnat')
        assert not result['actions'][0]['connected'] and len(h.events('accept'))==before
        n.snapshot('translated-destination-denied')
        h.cases.append(dict(case='post-dnat-deny-'+family,receiver_accepts=0))
    future=h.instance(18);future.boot();future.reject_install(lambda x:x.update(committed_at=now_string(60)),'future-commit-proof')
    routing=h.instance(19);routing.boot();h.exec(routing.run,'ip','rule','add','priority','10','blackhole');routing.reject_install(lambda x:None,'policy-route-drift')
    dns_expiry=h.instance(20);dns_expiry.boot(deadline=now_string(7))
    assert all(x['success'] for x in dns_expiry.actor(dns,'resolver-before-hard-deadline')['actions'])
    for p in dns_expiry.proc.values():p.stdin.close();assert p.wait(timeout=3)==0
    time.sleep(7.1)
    before=len(h.events('dns'))
    assert not any(x['success'] for x in dns_expiry.actor(dns,'resolver-after-hard-deadline')['actions'])
    assert len(h.events('dns'))==before
    h.cases.append(dict(case='resolver-also-expires-without-supervisor',receiver_dns_after_expiry=0))
    drift=h.instance(21);drift.boot()
    h.exec(drift.run,'nft','-f','-',data=b'delete element inet dp_run resolver4 { 192.0.2.53 . 53 }\nadd element inet dp_run resolver4 { 192.0.2.53 . 53 timeout 1h }\n')
    drift.reject_install(lambda x:None,'expiry-drift')
    h.cases.append(dict(case='no-physical-release-proof',physical_release_claimed=False,requires_task65=True))


def main():
    p=argparse.ArgumentParser()
    p.add_argument('--evidence',required=True,type=Path)
    p.add_argument('--supervisor',required=True,type=Path)
    p.add_argument('--pg-binary',type=Path)
    p.add_argument('--mode',choices=['normal','race'],default='normal')
    a=p.parse_args()
    h=Harness(a.evidence.resolve(),a.supervisor)
    status=1
    try:
        if a.pg_binary:h.pg(a.pg_binary.resolve(),a.mode)
        else:suite(h)
        status=0
    finally:
        h.record('result',dict(exit_code=status,task='6.4',no_production_network=True,physical_stop=False))
        h.close()
    return status


if __name__=='__main__':
    sys.exit(main())
