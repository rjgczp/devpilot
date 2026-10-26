#!/usr/bin/env python3
"""Reuse the accepted real joint fixture; only select result-specific PG tests."""
import argparse
from pathlib import Path
from joint_fixture import JointHarness

class ResultHarness(JointHarness):
 def pg_test_name(self):return '^'+self.result_test+'$'
 def pg_completion(self,mode,status):
  if self.result_test=='TestResultPreflightPG':
   assert not self.events() and self.life is None
   self.record('preflight-zero-target',dict(target_tcp=0,get_count=0,probe=False,physical_release=False))
  return dict(test=self.result_test,case=self.case,mode=mode,exit_code=status,production_execution=False)

def main():
 p=argparse.ArgumentParser();p.add_argument('--evidence',type=Path,required=True);p.add_argument('--supervisor',type=Path,required=True);p.add_argument('--probe',type=Path,required=True);p.add_argument('--pg-binary',type=Path,required=True);p.add_argument('--mode',choices=['normal','race'],required=True);p.add_argument('--case',required=True);p.add_argument('--test',choices=['TestResultMinimalPG','TestResultUnknownCommitPG','TestResultActualLeaseExpiryPG','TestResultAfterOwnerPatchPG','TestResultTerminalUnknownPG','TestResultControlRacePG','TestResultPreflightPG','TestResultClockFloorPG'],required=True);a=p.parse_args()
 h=ResultHarness(a.evidence.resolve(),a.supervisor,a.probe,a.case,a.mode);h.result_test=a.test;status=1
 try:h.pg(a.pg_binary.resolve(),a.mode);status=0
 finally:h.record('result71-result',dict(exit_code=status,test=a.test,case=a.case,mode=a.mode));h.close()
 return status
if __name__=='__main__':raise SystemExit(main())
