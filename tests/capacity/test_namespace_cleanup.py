import json,sys,tempfile,unittest
from pathlib import Path
from types import SimpleNamespace
sys.path.insert(0,str(Path(__file__).resolve().parent))
from continuous import CapacityHarness,LABEL

class NamespaceCleanup(unittest.TestCase):
 def test_cleanup_uses_owned_ids_and_keeps_unknown(self):
  # Allocation/outer cleanup only: no SQL, physical receipt or slot return.
  h=object.__new__(CapacityHarness);h.prefix='test';h.cycle=1
  h.active=SimpleNamespace(name='run-1',run='a'*64,gateway='b'*64,actor_container=None)
  held='c'*64;h.containers=[held,h.active.run,h.active.gateway];removed=[]
  def command(args):
   cid=args[-1]
   self.assertNotEqual(cid,held)
   if args[1]=='inspect':
    suffix='-guard' if cid==h.active.gateway else ''
    return json.dumps([dict(Name='/test-run-1'+suffix,Config=dict(Labels={LABEL:'test'}))]).encode()
   self.assertEqual(args[:3],['docker','rm','-f']);removed.append(cid);return b''
  h.run=command
  with tempfile.TemporaryDirectory() as d:
   h.dest=Path(d);h.archive_cycle()
  self.assertEqual(h.containers,[held]);self.assertEqual(set(removed),{h.active.run,h.active.gateway})

if __name__=='__main__':unittest.main()
