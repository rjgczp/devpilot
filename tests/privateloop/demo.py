#!/usr/bin/env python3
"""Interactive local private mode; never production or Internet execution."""
import argparse, os, signal, subprocess, sys
from pathlib import Path
from loop import LoopHarness
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'demo'))
from demo import DemoHarness

class PrivateDemo(LoopHarness):
 demo=DemoHarness.demo
 demo_binary='private-loop-pg.test'
 demo_pattern='TestPrivateLoopPG'
 def public_catalog(self,selected):
  return dict(super().public_catalog(selected),case='web')
 def pg_operation(self,instance,op,payload,selected,mode):
  if op.startswith('demo-'):
   return DemoHarness.pg_operation(self,instance,op,payload,selected,mode)
  return super().pg_operation(instance,op,payload,selected,mode)

def main():
 p=argparse.ArgumentParser()
 for name in ['tools','evidence','credentials','stop-file']:p.add_argument('--'+name,type=Path,required=True)
 a=p.parse_args();os.umask(0o022)
 h=PrivateDemo(a.evidence.resolve(),a.tools.resolve(),'normal')
 h.credentials=a.credentials.resolve();h.stop_file=a.stop_file.resolve()
 def stop(*_):raise KeyboardInterrupt
 signal.signal(signal.SIGTERM,stop)
 status=1
 try:h.demo(a.tools.resolve(),900);status=0
 except KeyboardInterrupt:status=130
 finally:
  h.record('runner-exit',dict(exit_code=status,production_execution=False,teardown_is_not_physical_proof=True))
  h.containers=[cid for cid in h.containers if subprocess.run(['docker','inspect',cid],capture_output=True).returncode==0]
  h.close();h.credentials.unlink(missing_ok=True)
 return status
if __name__=='__main__':raise SystemExit(main())
