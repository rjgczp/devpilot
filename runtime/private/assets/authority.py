import base64,datetime,hashlib,json,os,subprocess,tempfile,uuid
from pathlib import Path

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

