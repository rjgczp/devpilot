"""Subprocess boundaries exercise actual -O and PYTHONOPTIMIZE interpretation."""
import os,pathlib,subprocess,sys,unittest
ROOT=pathlib.Path(__file__).resolve().parents[2]
class Optimization(unittest.TestCase):
 def test_negative_admission_release_and_syscall_checks_in_all_modes(self):
  for flags,value,level in [([],None,0),(['-O'],None,1),([],'1',1),([],'2',2)]:
   with self.subTest(flags=flags,PYTHONOPTIMIZE=value):
    env=dict(os.environ);env.pop('PYTHONOPTIMIZE',None)
    if value is not None:env['PYTHONOPTIMIZE']=value
    p=subprocess.run([sys.executable,'-B','-S',*flags,str(ROOT/'tests/privateservice/optimized_checks.py')],env=env,capture_output=True,text=True,timeout=30)
    self.assertEqual(p.returncode,0,p.stdout+p.stderr)
    self.assertIn('"optimization": '+str(level),p.stdout)
    self.assertIn('Ran 7 tests',p.stderr)
if __name__=='__main__':unittest.main()
