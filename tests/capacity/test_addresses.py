import ipaddress,sys,unittest
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'scheduler'))
from network_fixture import backend_addresses
class CapacityAddressPlan(unittest.TestCase):
 def test_held_unknown_plus105_do_not_overlap_probe_addresses(self):
  back=[];probes=[]
  for i in range(106):
   a,b=backend_addresses(i,128);back.extend([ipaddress.ip_address(a),ipaddress.ip_address(b)])
   probes.extend([ipaddress.ip_address(f'198.19.{64+i//16}.{(i%16)*16+2}'),ipaddress.ip_address(f'198.19.{64+i//16}.{(i%16)*16+3}')])
  self.assertEqual(len(set(back)),212);self.assertTrue(set(back).isdisjoint(probes))
  self.assertEqual(backend_addresses(0),('198.19.66.1','198.19.66.2'))
  self.assertEqual(backend_addresses(2),('198.19.66.9','198.19.66.10'))
if __name__=='__main__':unittest.main()
