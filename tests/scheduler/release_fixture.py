#!/usr/bin/env python3
"""5.4 only: real suspended Supervisor/zombie, then real stop and PG settlement."""
import argparse,base64,hashlib,json,subprocess,time
from pathlib import Path
from termination_fixture import StopHarness,Lifecycle,canonical

class ReleaseHarness(StopHarness):
    def pg_test_name(self):return '^TestBudgetPhysicalReleasePG$'
    def pg_completion(self,mode,status):
        return dict(case='physical-release-pg18-'+mode,exit_code=status,physical_release_verified=True,task63_joint_accepted=False)
    def pg_operation(self,instance,op,payload,selected,mode):
        if op=='zombie':
            life=Lifecycle(self,instance);self.lifecycle.append(life);life.launch(selected);self.life=life
            life.call('inspect')
            # Freeze only the independent lifecycle PID1, not the Probe tree.
            # Kernel network TTL still expires at its original fixed deadline.
            self.run(['docker','kill','--signal','STOP',life.name])
            return self.zombie_snapshot('before-real-lease-expiry')
        if op=='stop-zombie':
            live=self.zombie_snapshot('after-real-lease-expiry')
            assert len(live['live_pids'])>=3
            proofs=self.life.revoke(require_live=False)
            self.run(['docker','kill','--signal','CONT',self.life.name])
            # SIGCONT alone is not a stop acknowledgement. Wake the existing
            # bounded control protocol explicitly and require every real proof.
            receipt=self.life.stop(proofs,consume=False)
            bundle=json.loads((self.authority.path/(self.life.identity['run_id']+'.proofs')).read_bytes())
            self.record(instance.name+'-committed-bundle',bundle)
            self.record('private-journal-commit',dict(run_id=self.life.identity['run_id'],bundle_sha256=hashlib.sha256(canonical(bundle)).hexdigest(),receipt_sha256=hashlib.sha256(canonical(receipt)).hexdigest(),committed_before_database_accept=True))
            return dict(stop_receipt=receipt,stop_bundle=bundle)
        raise RuntimeError('unknown release operation')
    def zombie_snapshot(self,phase):
        code="""import os,json,pathlib
rows=[]
for p in pathlib.Path('/proc').iterdir():
 if not p.name.isdecimal() or int(p.name)==os.getpid():continue
 try:
  text=(p/'status').read_text();fields=dict((k,v.strip()) for l in text.splitlines() if ':' in l for k,v in [l.split(':',1)])
  rows.append(dict(pid=int(p.name),status={k:fields[k] for k in ['State','PPid','Uid','CapEff','NoNewPrivs']}))
 except FileNotFoundError:pass
print(json.dumps(rows))"""
        rows=json.loads(self.exec(self.life.name,'python3','-c',code))
        init=next(x for x in rows if x['pid']==1);assert init['status']['State'].startswith('T')
        children=[x for x in rows if x['pid']>1 and x['status']['State'][0] not in 'ZT']
        assert len(children)>=3 and all(x['status']['Uid'].split()[0]=='65534' and int(x['status']['CapEff'],16)==0 for x in children)
        self.record('zombie-'+phase,dict(supervisor=init,live_pids=children,at=time.time_ns()))
        return dict(live_pids=[x['pid'] for x in children])

def main():
    p=argparse.ArgumentParser();p.add_argument('--evidence',type=Path,required=True);p.add_argument('--supervisor',type=Path,required=True);p.add_argument('--pg-binary',type=Path,required=True);p.add_argument('--mode',choices=['normal','race'],required=True);a=p.parse_args()
    h=ReleaseHarness(a.evidence.resolve(),a.supervisor);status=1
    try:h.pg(a.pg_binary.resolve(),a.mode);status=0
    finally:
        h.record('release54-result',dict(exit_code=status,task='5.4',task63_joint_accepted=False));h.close()
    return status
if __name__=='__main__':raise SystemExit(main())
