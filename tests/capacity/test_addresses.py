import ipaddress,sys,unittest
from types import SimpleNamespace
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scheduler'))
from network_fixture import backend_addresses,probe_addresses
from continuous import CapacityHarness
class CapacityAddressPlan(unittest.TestCase):
 def test_held_unknown_plus105_do_not_overlap_probe_addresses(self):
  back=[];probes=[];h=object.__new__(CapacityHarness);h.completed=[];h.containers=[];h.instances=[]
  for i in range(106):
   a,b=backend_addresses(i,128);back.extend([ipaddress.ip_address(a),ipaddress.ip_address(b)])
   addresses=probe_addresses(h.probe4_index(i));probes.extend(map(ipaddress.ip_address,addresses))
   self.assertTrue(all(ipaddress.ip_address(x) in ipaddress.ip_network('198.19.64.0/23') for x in addresses))
   if i:
    h.completed.append({});h.instances.append(SimpleNamespace(index=i,run='prior-run',gateway='prior-guard'))
  self.assertEqual(len(set(back)),212);self.assertTrue(set(back).isdisjoint(probes))
  self.assertEqual(backend_addresses(0),('198.19.66.1','198.19.66.2'))
  self.assertEqual(backend_addresses(2),('198.19.66.9','198.19.66.10'))
  self.assertEqual(len(set(probes)),4) # Held unknown pair + sequential pair.
  with self.assertRaises(ValueError):probe_addresses(32)
 def test_reuse_refuses_unsettled_or_still_present_namespace(self):
  h=object.__new__(CapacityHarness);h.completed=[];h.instances=[];h.containers=[]
  with self.assertRaises(AssertionError):h.probe4_index(2)
  h.completed=[{}];h.instances=[SimpleNamespace(index=1,run='old',gateway='guard')];h.containers=['old']
  with self.assertRaises(AssertionError):h.probe4_index(2)
if __name__=='__main__':unittest.main()
