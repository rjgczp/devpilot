#!/usr/bin/env python3
"""Task6.5 fixture-only trusted lifecycle Supervisor and real Linux acceptance.

Worker/Probe never receives Docker, signing key or private management pipes.
No DB release or result authority is added. A receipt is signed only after
independent network proof, pidfd/reap proof and irreversible PID1 retirement.
"""
import argparse,base64,datetime,hashlib,json,os,re,select,subprocess,tempfile,time,uuid
from pathlib import Path
from network_fixture import Harness,HERE,IMAGE,LABEL,now_string


def canonical(value):return json.dumps(value,sort_keys=True,separators=(',',':')).encode()
def digest(value):return hashlib.sha256(canonical(value)).hexdigest()


def sync_directory(path):
    fd=os.open(path,os.O_RDONLY|os.O_DIRECTORY)
    try:os.fsync(fd)
    finally:os.close(fd)


def valid_identity(identity):
    if len(identity)!=10:return False
    for key,value in identity.items():
        if not isinstance(value,str):return False
        try:
            if key.endswith(('version','generation','epoch','token')):
                if not (str(int(value))==value and 0<int(value)<=9223372036854775807):return False
            elif str(uuid.UUID(value))!=value or uuid.UUID(value).int==0:return False
        except (ValueError,TypeError):return False
    return set(identity)=={'workspace_id','project_id','monitor_id','run_id','configuration_version','activation_generation','scheduler_epoch','fencing_token','namespace_incarnation','supervisor_incarnation'}


class Authority:
    def __init__(self,state=None):
        # A private management domain, never mounted or inherited by a child.
        self.directory=tempfile.TemporaryDirectory(prefix='devpilot-stop65-key-') if state is None else None
        self.path=Path(self.directory.name) if self.directory else Path(state)
        metadata=self.path.stat();assert metadata.st_mode&0o777==0o700 and metadata.st_uid==os.getuid()
        self.key=self.path/'private.pem';self.pub=self.path/'public.pem'
        if not self.key.exists():
            subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(self.key)],check=True,capture_output=True)
        os.chmod(self.key,0o600)
        subprocess.run(['openssl','pkey','-in',str(self.key),'-pubout','-out',str(self.pub)],check=True,capture_output=True)
        self.key_id=hashlib.sha256(self.pub.read_bytes()).hexdigest()
        sync_directory(self.path)
        self.pending={}

    def register(self,identity):
        assert valid_identity(identity)
        run=identity['run_id']
        # Exclusive persistent burn precedes allocation; no replacement node can
        # get a second capability for the same Run, even after a lost ACK.
        fd=os.open(self.path/run,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb') as f:f.write(canonical(identity));f.flush();os.fsync(f.fileno())
        sync_directory(self.path)
        self.pending[run]=identity.copy()

    def sign(self,identity,network,process,retired):
        assert self.pending[identity['run_id']]==identity and len(network)==2
        assert {x['role'] for x in network}=={'run','gateway'}
        assert all(x['lineage']==identity and x['link_removed'] and x['conntrack_empty'] for x in network)
        assert len({x['namespace'] for x in network})==2
        assert process['lineage']==identity and process['remaining_pids']==[] and process['launch_consumed']
        assert process['pids'] and all(x['pidfd_exited'] for x in process['pids'])
        assert all(datetime.datetime.fromisoformat(x['stopped_at'].replace('Z','+00:00')).timestamp()*1e9<=process['at_ns'] for x in network)
        assert process['net_namespace']==next(x['namespace'] for x in network if x['role']=='run')
        assert retired['event']=='pid_namespace_retiring' and retired['lineage']==identity and retired['pid_namespace']==process['pid_namespace']
        payload=dict(protocol='fixture-physical-stop-v1',receipt_id=str(uuid.uuid4()),lineage=identity,
                     supervisor_key_id=self.key_id,network_sha256=digest(network),process_sha256=digest(process),
                     retire_sha256=digest(retired),pid_namespace=process['pid_namespace'],
                     net_namespace=process['net_namespace'],process_count=len(process['pids']),at=datetime.datetime.fromtimestamp(process['at_ns']/1e9,datetime.timezone.utc).isoformat())
        # Commit the exact independent proofs before a signed attestation exists.
        # Private immutable journal files are not Worker reports or Docker cleanup.
        bundle=dict(network=network,process=process,retired=retired)
        fd=os.open(self.path/(identity['run_id']+'.proofs'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb') as f:f.write(canonical(bundle));f.flush();os.fsync(f.fileno())
        sync_directory(self.path)
        raw=canonical(payload);assert len(raw)<=4096
        content=self.path/'payload';content.write_bytes(raw)
        sig=subprocess.run(['openssl','pkeyutl','-sign','-rawin','-inkey',str(self.key),'-in',str(content)],capture_output=True,check=True).stdout
        # No repeated signing after a partial/lost delivery; reconciliation can
        # only read this identical immutable receipt, never run the Probe again.
        receipt=dict(payload=payload,signature=base64.b64encode(sig).decode())
        fd=os.open(self.path/(identity['run_id']+'.receipt'),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
        with os.fdopen(fd,'wb') as f:f.write(canonical(receipt));f.flush();os.fsync(f.fileno())
        sync_directory(self.path)
        del self.pending[identity['run_id']]
        return receipt

    def verify(self,receipt,identity):
        payload=receipt['payload']
        if not valid_identity(identity):return False
        try:
            if str(uuid.UUID(payload['receipt_id']))!=payload['receipt_id']:return False
        except (KeyError,ValueError,TypeError):return False
        if payload.get('lineage')!=identity or payload.get('supervisor_key_id')!=self.key_id or payload.get('protocol')!='fixture-physical-stop-v1':return False
        if (self.path/('accepted-'+payload['receipt_id'])).exists():return False
        content=self.path/'verify-payload';sig=self.path/'verify-signature'
        content.write_bytes(canonical(payload))
        try:sig.write_bytes(base64.b64decode(receipt['signature'],validate=True))
        except (ValueError,TypeError):return False
        r=subprocess.run(['openssl','pkeyutl','-verify','-rawin','-pubin','-inkey',str(self.pub),'-in',str(content),'-sigfile',str(sig)],capture_output=True)
        if r.returncode:return False
        try:
            fd=os.open(self.path/('accepted-'+payload['receipt_id']),os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600)
            with os.fdopen(fd,'wb') as f:f.write(canonical(receipt));f.flush();os.fsync(f.fileno())
        except FileExistsError:return False
        sync_directory(self.path)
        return True


class Lifecycle:
    def __init__(self,h,instance):
        self.h,self.instance=h,instance;self.identity=instance.identity.copy();self.stopped=False
        h.authority.register(self.identity)
        boundary=(HERE/'network_actor.py').read_text()
        source="import types,sys\nm=types.ModuleType('network_actor')\nexec("+repr(boundary)+",m.__dict__)\nsys.modules['network_actor']=m\n"+(HERE/'termination_actor.py').read_text()
        init='ACTOR_SOURCE='+repr(source)+'\n'+(HERE/'termination_init.py').read_text()
        cmd=['docker','run','--rm','-i','--name',h.prefix+'-'+instance.name+'-lifecycle','--label',LABEL+'='+h.prefix,
             '--network','container:'+instance.run,'--cap-drop','ALL','--cap-add','KILL','--cap-add','SETUID','--cap-add','SETGID','--cap-add','SETPCAP',
             '--security-opt','no-new-privileges:true','--read-only','--memory=128m','--cpus=1','--pids-limit=128',
             '--ulimit','nofile=256:256','--tmpfs','/tmp:rw,noexec,nosuid,size=4m','--tmpfs','/var/lib/postgresql:ro,noexec,nosuid,size=4k',
             '--entrypoint','python3',IMAGE,'-u','-c',init]
        # Log the pinned script hash, not IPC payloads or a signing key.
        h.record(instance.name+'-lifecycle-command',dict(command=cmd[:-1]+['<pinned-init-source>'],source_sha256=hashlib.sha256(init.encode()).hexdigest()))
        self.proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=open(h.dest/(instance.name+'-lifecycle.stderr'),'wb'))
        h.processes.append(self.proc)
        deadline=int(datetime.datetime.fromisoformat(instance.config['run']['hard_deadline'].replace('Z','+00:00')).timestamp()*1e9)
        self.send(dict(op='boot',lineage=self.identity,deadline_ns=deadline))
        self.ready=h.readline(self.proc);assert self.ready['event']=='lifecycle_ready'
        self.name=h.prefix+'-'+instance.name+'-lifecycle'
        self.container=json.loads(h.run(['docker','inspect',self.name]))[0]
        h.record(instance.name+'-lifecycle-container',self.container)
        assert self.container['HostConfig']['PidMode']=='' and not self.container['HostConfig']['Privileged'] and not self.container['HostConfig']['Binds']
        assert self.ready['net_namespace']==instance.ready['run']['namespace']

    def send(self,value):
        raw=json.dumps(value).encode()+b'\n';assert len(raw)<=4096
        self.proc.stdin.write(raw);self.proc.stdin.flush()

    def call(self,op,**values):
        self.send(dict(op=op,lineage=self.identity,**values));answer=self.h.readline(self.proc)
        self.h.record(self.instance.name+'-'+op,answer);return answer

    def launch(self,ip):
        answer=self.call('launch',config=dict(ip=ip,certificate=self.h.certificate,gateway=self.instance.gate4))
        assert answer['event']=='tls_ready';assert self.h.events('stream_open')[-1]['sni']=='a.fixture.example.com'
        frames=self.h.events('stream_frame');assert frames[-1]['frame']=='before-stop'
        return answer

    def revoke(self,require_live=True):
        lower=None
        if require_live:
            install=json.loads((self.h.dest/(self.instance.name+'-install-request.json')).read_text())
            group='target6' if ':' in install['request']['ip'] else 'target4'
            code="import json,subprocess,time; before=time.time_ns(); text=subprocess.check_output(['nft','list','set','inet','dp_run',"+repr(group)+"]).decode(); print(json.dumps(dict(before_ns=before,rules=text)))"
            sample=json.loads(self.h.exec(self.instance.gateway,'python3','-c',code))
            expiry=re.findall(r'expires ([0-9a-z]+)',sample['rules']);assert len(expiry)==1,'existing stream stop must precede natural permit expiry'
            parts=re.findall(r'(\d+)(ms|s|m|h|d)',expiry[0]);seconds=sum(int(n)*{'ms':.001,'s':1,'m':60,'h':3600,'d':86400}[u] for n,u in parts)
            lower=sample['before_ns']/1e9+seconds-.001
            self.h.record(self.instance.name+'-live-permit-before-stop',dict(**sample,conservative_expiry_lower=lower))
        proofs=[]
        for role,cid in [('gateway',self.instance.gateway),('run',self.instance.run)]:
            request=json.dumps(dict(op='revoke',config=self.instance.config[role])).encode()+b'\n'
            raw=self.h.exec(cid,'/usr/local/bin/networksupervisorfixture',data=request)
            proof=json.loads(raw);assert proof['lineage']==self.identity and proof['role']==role
            if role=='gateway' and lower is not None:assert datetime.datetime.fromisoformat(proof['stopped_at'].replace('Z','+00:00')).timestamp()<lower,'natural expiry cannot stand in for emergency stop'
            self.h.record(self.instance.name+'-'+role+'-network-stop',proof);proofs.append(proof)
        self.instance.snapshot('physically-disconnected')
        return proofs

    def stop(self,network=None,consume=True):
        network=network or self.revoke()
        process=self.call('stop');assert process['event']=='processes_terminated'
        trace=bytearray()
        while True:
            self.send(dict(op='trace',lineage=self.identity,offset=len(trace)))
            frame=self.h.readline(self.proc);assert frame['event']=='trace' and frame['offset']==len(trace)
            trace+=base64.b64decode(frame['chunk'],validate=True)
            if len(trace)==frame['total']:break
            assert len(trace)<frame['total']<=65536
        assert hashlib.sha256(trace).hexdigest()==frame['sha256']
        (self.h.dest/(self.instance.name+'-probe.trace')).write_bytes(trace)
        retired=self.call('retire');self.proc.stdin.close();assert self.proc.wait(timeout=3)==0
        # PID1 retired after every pidfd became readable and after empty /proc.
        # Container removal is later teardown, never this physical proof.
        self.stopped=True
        receipt=self.h.authority.sign(self.identity,network,process,retired)
        self.h.record(self.instance.name+'-termination-receipt',receipt)
        if consume:
            assert self.h.authority.verify(receipt,self.identity)
            assert not self.h.authority.verify(receipt,self.identity)
        self.h.receipt=receipt
        return receipt


class StopHarness(Harness):
    def __init__(self,dest,binary):
        super().__init__(dest,binary);self.authority=Authority()
        self.record('receipt-public-key',dict(key_id=self.authority.key_id,pem=self.authority.pub.read_text()))
        self.lifecycle=[]

    def pg_test_name(self):
        return '^TestBudgetPhysicalStopPG$'

    def public_catalog(self,selected):
        return dict(ip=selected,stop_public_key=self.authority.pub.read_text())

    def pg_witness(self,instance,selected,mode):
        life=Lifecycle(self,instance);self.lifecycle.append(life)
        life.launch(selected)
        before=len(self.events('stream_frame'));proof=life.revoke()
        attempt=life.call('write-after-cut');assert attempt['event']=='write_attempt'
        time.sleep(.6)
        assert len(self.events('stream_frame'))==before
        life.call('inspect');receipt=life.stop(proof)
        self.cases.append(dict(case='real-pg18-physical-stop-'+mode,established_flow_cut=True,receipt_verified=True,capacity_released=False))
        return dict(fixture_teardown_complete=False,stop_receipt=receipt)

    def close(self):
        # Emergency test cleanup cannot produce a receipt or release a slot.
        for life in self.lifecycle:
            if not life.stopped and life.proc.poll() is None:
                try:life.proc.stdin.close();life.proc.wait(timeout=3)
                except (OSError,subprocess.TimeoutExpired):pass
        for life in self.lifecycle:
            found=subprocess.run(['docker','inspect',life.name],capture_output=True)
            if found.returncode==0:
                d=json.loads(found.stdout)[0];assert d['Config']['Labels'][LABEL]==self.prefix
                self.run(['docker','rm','-f','-v',life.name])
        super().close();self.authority.directory.cleanup()


def suite(h):
    h.setup_receiver()
    for index,ip in [(0,'93.184.216.34'),(1,'2001:4860:ffff::34')]:
        instance=h.instance(index);instance.boot();instance.install(ip=ip,port=443,seconds=24)
        life=Lifecycle(h,instance);h.lifecycle.append(life);life.launch(ip)
        snap=life.call('inspect');assert len(snap['pids'])>=3
        # Ignore or kill the Worker: no Worker exists in this stop path at all.
        if index:
            for p in instance.proc.values():p.stdin.close();assert p.wait(timeout=3)==0
            h.exec(instance.run,'nft','delete','table','inet','dp_run')
        count=len(h.events('stream_frame'));network=life.revoke()
        life.call('write-after-cut');time.sleep(.6)
        assert len(h.events('stream_frame'))==count
        receipt=life.stop(network)
        assert receipt['payload']['process_count']>=3
        h.cases.append(dict(case='tls-established-independent-stop-ip'+str(4 if index==0 else 6),existing_stream_cut=True,all_pidfds_exited=True,orphan_reaped=True))
        # Replaying an old binding into a new physical namespace cannot restore
        # the old capability. Run registration remains burned across recovery.
        try:h.authority.register(life.identity)
        except FileExistsError:pass
        else:raise AssertionError('old Run rebound')
        for key in life.identity:
            other=life.identity.copy();other[key]=('2' if key.endswith(('version','generation','epoch','token')) else str(uuid.uuid4()))
            assert not h.authority.verify(receipt,other)
        forged=json.loads(json.dumps(receipt));forged['payload']['receipt_id']=str(uuid.uuid4())
        assert not h.authority.verify(forged,life.identity)
        foreign=Authority()
        assert not foreign.verify(receipt,life.identity)
        foreign.directory.cleanup()
        recovered=Authority(h.authority.path)
        assert not recovered.verify(receipt,life.identity)
        try:recovered.register(life.identity)
        except FileExistsError:pass
        else:raise AssertionError('node restart revived old Run')
    h.cases.append(dict(case='repeated-forged-foreign-key-old-fence-receipts',rejected=True))
    # Both policy tables can be missing at emergency time. The independent
    # revoker binds the private bootstrap ledger and rebuilds terminal DROP,
    # then physically removes the wire; it need not trust the lost live policy.
    damaged=h.instance(2);damaged.boot();damaged.install(port=443,seconds=24)
    life=Lifecycle(h,damaged);h.lifecycle.append(life);life.launch('93.184.216.34')
    h.exec(damaged.run,'nft','delete','table','inet','dp_run')
    h.exec(damaged.gateway,'nft','delete','table','inet','dp_run')
    count=len(h.events('stream_frame'));proofs=life.revoke(require_live=False)
    life.call('write-after-cut');time.sleep(.6);assert len(h.events('stream_frame'))==count
    life.stop(proofs)
    h.cases.append(dict(case='lost-both-rules-independent-revocation',stream_cut=True,receipt_verified=True))
    # A dead lifecycle Supervisor cannot produce pidfd/reap confirmation.
    # Docker fault injection/teardown is deliberately NOT accepted as evidence.
    lost=h.instance(3);lost.boot();lost.install(ip='2001:4860:ffff::34',port=443,seconds=24)
    life=Lifecycle(h,lost);h.lifecycle.append(life);life.launch('2001:4860:ffff::34')
    h.run(['docker','kill','--signal','KILL',life.name]);assert life.proc.wait(timeout=3)!=0
    life.revoke()
    assert life.identity['run_id'] in h.authority.pending
    assert not (h.authority.path/(life.identity['run_id']+'.receipt')).exists()
    rebuilt=life.identity.copy();rebuilt['namespace_incarnation']=str(uuid.uuid4());rebuilt['supervisor_incarnation']=str(uuid.uuid4())
    recovered=Authority(h.authority.path)
    try:recovered.register(rebuilt)
    except FileExistsError:pass
    else:raise AssertionError('unconfirmed old node replaced')
    h.cases.append(dict(case='supervisor-loss-quarantines-no-receipt',capacity_released=False,old_run_recovery_rejected=True))
    # A newly allocated namespace cannot accept old control messages; the old
    # private kernel ledger also forbids bootstrapping its retired namespace.
    fresh=h.instance(4);fresh.boot()
    request=json.dumps(dict(op='revoke',config=lost.config['run'])).encode()+b'\n'
    failure=subprocess.run(['docker','exec','-i',fresh.run,'/usr/local/bin/networksupervisorfixture'],input=request,capture_output=True,timeout=3)
    assert failure.returncode==2 and not failure.stdout
    h.record('cross-node-old-control-rejected',dict(exit_code=failure.returncode,stderr=failure.stderr.decode()))
    assert json.loads(h.exec(fresh.run,'ip','-j','link','show','eth0'))[0]['ifname']=='eth0'
    # Previously valid old installation cannot recover the detached old wire.
    cfg=lost.config['run'].copy();cfg['hard_deadline']=now_string(30)
    restart=subprocess.run(['docker','exec','-i',lost.run,'/usr/local/bin/networksupervisorfixture'],input=json.dumps(dict(op='boot',config=cfg)).encode()+b'\n',capture_output=True,timeout=3)
    assert restart.returncode==2 and not restart.stdout
    h.record('retired-node-bootstrap-rejected',dict(exit_code=restart.returncode,stderr=restart.stderr.decode()))
    h.cases.append(dict(case='namespace-rebuild-old-node-control-replay',rejected=True))


def main():
    parser=argparse.ArgumentParser();parser.add_argument('--evidence',type=Path,required=True);parser.add_argument('--supervisor',type=Path,required=True)
    parser.add_argument('--pg-binary',type=Path);parser.add_argument('--mode',choices=['normal','race'],default='normal');a=parser.parse_args()
    h=StopHarness(a.evidence.resolve(),a.supervisor);status=1
    try:
        if a.pg_binary:h.pg(a.pg_binary.resolve(),a.mode)
        else:suite(h)
        status=0
    finally:
        h.record('stop65-result',dict(exit_code=status,task='6.5',database_release_accepted=False,task63_joint_accepted=False))
        h.close()
    return status


if __name__=='__main__':raise SystemExit(main())
