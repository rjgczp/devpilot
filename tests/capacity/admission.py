#!/usr/bin/env python3
"""Fresh disposable PG18: exact approved schema/protocol/ACL only, no GET."""
import argparse,json,os,subprocess,sys
from pathlib import Path
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'privateloop'))
from loop import LoopHarness
def main():
 p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);a=p.parse_args()
 h=LoopHarness(a.evidence.resolve(),a.tools.resolve(),'normal');status=1
 try:
  h.setup_control();cmd=h.control_command(h.prefix+'-coordinator','private-loop-pg.test','^TestCapacityAdmissionPG$',source=True)
  proc=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.STDOUT);h.processes.append(proc)
  raw,_=proc.communicate(timeout=90);(h.dest/'admission-driver.log').write_bytes(raw);status=proc.returncode
  assert status==0,'inspect preserved admission-driver.log'
  assert not h.events('get')
  h.record('result',dict(exit_code=0,scope='injected protocol thresholds, real5s TTL and ACL',actual_gets=0,storage_capacity_proof=False,production=False))
 finally:
  h.record('exit',dict(exit_code=status));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
