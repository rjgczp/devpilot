import datetime
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
ROOT=Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'runtime/private_lite'))
from egress import HostFilter, public4
from namespace import NamespaceExecutor
import os
class Profiles(unittest.TestCase):
 def test_real_host_namespace_is_rejected_before_privileged_start(self):
  if sys.platform!='linux':self.skipTest('Linux-only namespace identity')
  f=HostFilter(None,'test','lite0','198.18.2.2','1.1.1.1','93.184.216.34','wan0')
  with self.assertRaises(ValueError):NamespaceExecutor(os.getpid(),os.readlink('/proc/self/ns/net'),['lite0','wan0'],f.tables)
 def test_default_closed_before_config_or_docker(self):
  if sys.platform!='linux':self.skipTest('Linux-only entry')
  with tempfile.TemporaryDirectory() as directory:
   result=subprocess.run([sys.executable,'-S',str(ROOT/'runtime/private_lite/launcher.py'),'run','--state',directory,'--config','/missing/private-lite.json'],capture_output=True,timeout=5)
   self.assertEqual(result.returncode,0,result.stderr)
   self.assertFalse(json.loads(result.stdout)['execution_available'])
 def test_endpoint_and_interface_injection_are_rejected(self):
  for value in ('192.0.2.53','169.254.169.254','127.0.0.1','::1','10.0.0.1'):
   with self.assertRaises(ValueError):public4(value)
  with self.assertRaises(ValueError):HostFilter(None,'test','x" accept','198.18.2.2','1.1.1.1','93.184.216.34','wan0')
 def test_host_commands_are_scoped_and_do_not_edit_docker_rules(self):
  f=HostFilter(None,'test','lite0','198.18.2.2','1.1.1.1','93.184.216.34','wan0')
  s=f.script()
  self.assertNotIn('flush ruleset',s);self.assertNotIn('DOCKER',s)
  self.assertIn('hook postrouting',s);self.assertIn('hook prerouting',s)
  self.assertEqual(s.count('type ipv4_addr . inet_service'),2)
  self.assertNotIn('ct state established accept',s)
 def test_invalid_deadline_never_mutates_host_grants(self):
  calls=[]
  f=HostFilter(lambda *a,**kw:calls.append((a,kw)),'test','lite0','198.18.2.2','1.1.1.1','93.184.216.34','wan0')
  f.hashes=['expected'];f.snapshot=lambda:['expected']
  f.guard.check=lambda:None  # this test starts with an intact guard/policy
  for delta in (-1,40):
   end=datetime.datetime.now(datetime.timezone.utc)+datetime.timedelta(seconds=delta)
   with self.assertRaises(ValueError):f.grant(end.isoformat(),True)
  self.assertEqual(calls,[])
