import importlib.util,ipaddress,json,pathlib,sys,tempfile,unittest

ROOT=pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'runtime/private'))
from unittest import mock
import launcher
from kernel import Kernel

class Composition(unittest.TestCase):
 def test_original_stop_algorithm_and_signing_journal_are_reused(self):
  accepted=(ROOT/'tests/scheduler/termination_init.py').read_text().split('assert os.getpid()==1')[0]
  self.assertEqual((ROOT/'runtime/private/assets/termination_core.py').read_text(),accepted)
  self.assertEqual((ROOT/'runtime/private/assets/lifecycle.py').read_bytes(),(ROOT/'tests/scheduler/joint_init.py').read_bytes())
  self.assertEqual((ROOT/'runtime/private/assets/probe_bootstrap.py').read_bytes(),(ROOT/'tests/scheduler/joint_bootstrap.py').read_bytes())
  accepted=(ROOT/'tests/scheduler/termination_fixture.py').read_text();accepted=accepted[accepted.index('def canonical('):accepted.index('\n\nclass Lifecycle:')]
  self.assertIn(accepted,(ROOT/'runtime/private/assets/authority.py').read_text())
 def test_default_entry_does_not_start_docker_or_load_config(self):
  with tempfile.TemporaryDirectory() as d:
   old=sys.argv;sys.argv=['launcher','start','--state',d]
   oldplatform=sys.platform;sys.platform='linux'
   try:self.assertEqual(launcher.main(),0)
   finally:sys.argv=old;sys.platform=oldplatform
 def test_restart_can_refresh_public_key_without_changing_private_key(self):
  with tempfile.TemporaryDirectory() as d:
   state=pathlib.Path(d);state.chmod(0o700)
   with mock.patch.object(Kernel,'validate_target'):
    config=dict(target_container='test-only',image='test-only',runtime_id='test')
    first=Kernel(config,state);key=first.authority.key.read_bytes();public=first.authority.pub.read_bytes()
    self.assertEqual(first.authority.pub.stat().st_mode&0o777,0o444)
    again=Kernel(config,state)
    self.assertEqual(again.authority.key.read_bytes(),key);self.assertEqual(again.authority.pub.read_bytes(),public)
 def test_pending_recovery_reads_only_immutable_proof_and_never_launches(self):
  with tempfile.TemporaryDirectory() as d:
   k=object.__new__(Kernel);k.state=pathlib.Path(d)
   line=dict(run_id='fixed-run')
   (k.state/'fixed-run.owner.json').write_text(json.dumps(dict(index=0,line=line)))
   self.assertEqual(k._call(0,dict(op='pending'))['event'],'none')
   (k.state/'fixed-run.receipt').write_bytes(b'signed-receipt');(k.state/'fixed-run.proofs').write_bytes(b'committed-proofs')
   r=k._call(0,dict(op='pending'));self.assertEqual(r['event'],'pending');self.assertEqual(r['line'],line)
   self.assertEqual(k._call(1,dict(op='pending'))['event'],'none')
   k._call(0,dict(op='accepted',line=line));self.assertEqual(k._call(0,dict(op='pending'))['event'],'none')

if __name__=='__main__':unittest.main()
