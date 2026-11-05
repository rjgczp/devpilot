#!/usr/bin/env python3
"""Bounded local Web demo. Composes the accepted Task9.1 trusted fixture."""
import argparse
import base64
import json
import os
from pathlib import Path
import select
import secrets
import signal
import subprocess
import sys
import time

HERE = Path(__file__).resolve().parent
sys.path.insert(0,str(HERE.parent/'scheduler'))
from periodic91 import PeriodicHarness
from gateway import serve


class DemoHarness(PeriodicHarness):
    def pg_operation(self,instance,op,payload,selected,mode):
        if op == 'demo-bootstrap':
            return dict(frame=base64.b64encode(self.password.encode()).decode())
        if op == 'demo-ready':
            name=self.prefix+'-coordinator'
            info=json.loads(self.run(['docker','inspect',name]))[0]
            self.control_ids.add(name);self.containers.append(name)
            self.record('demo-api-coordinator',info)
            self.assert_wires()
            self.gateway = serve(name)
            self.metadata = payload
            private = self.credentials
            private.write_text(json.dumps(dict(email=payload['email'],password=self.password),indent=2)+'\n')
            private.chmod(0o600)
            self.record('demo',dict(**payload,url='http://127.0.0.1:5174',isolated=True,cycles=3,interval_seconds=60))
            print('隔离演示已就绪：http://127.0.0.1:5174\n临时 Owner 登录信息：'+str(private)+'\n请登录并点击“授权激活”。只允许固定无上游 fixture；三次真实60秒周期后停止新执行。',flush=True)
            return dict(event='demo_ready')
        if op == 'demo-completed':
            assert len(self.completed)==payload['cycles'] and 0<=payload['cycles']<=3
            print(str(payload['cycles'])+'次真实检查已完成，执行 gate 已关闭；页面仍可读取真实历史。',flush=True)
            return dict(event='demo_completed')
        if op == 'demo-view':
            # Retain real API and records, no more execution; bounded lifetime.
            while time.monotonic()<self.until:
                if self.stop_file.exists():break
                time.sleep(.25)
            return dict(event='view_finished')
        return super().pg_operation(instance,op,payload,selected,mode)

    def demo(self,tools,lifetime):
        self.password=secrets.token_urlsafe(30)
        self.until=time.monotonic()+lifetime
        self.pg_binary=tools/'periodic-pg.test'
        self.setup_control()
        cmd=self.control_command(self.prefix+'-coordinator',self.pg_binary.name,'TestWebDemoPG',source=True)
        cmd[-1]='-test.timeout=1100s'
        driver=subprocess.Popen(cmd,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
        self.processes.append(driver)
        # Coordinator is trusted control-only, never a Probe; original assertions
        # remain strict for PG and Workers and every data-plane namespace.
        logs=[]
        try:
            while driver.poll() is None or select.select([driver.stdout],[],[],0)[0]:
                if time.monotonic()>self.until:
                    raise RuntimeError('bounded demo lifetime ended')
                try:
                    line=self.read_bytes(driver,self.until)
                except RuntimeError as error:
                    if str(error)!='periodic process exited without completion':raise
                    driver.wait(timeout=5)
                    break
                logs.append(self.driver_log_line(line))
                if not line.startswith(b'NETWORK64 '):continue
                request=json.loads(line[len(b'NETWORK64 '):]);op,payload=request['op'],request['payload']
                if op=='catalog':answer=self.public_catalog('93.184.216.34')
                elif op=='prepare':answer=self.pg_prepare(None,payload)
                else:answer=self.pg_operation(None,op,payload,None,'normal')
                self.send(driver,answer)
            assert driver.wait(timeout=5)==0,'demo stopped conservatively; inspect driver evidence'
            self.record('demo-result',dict(exit_code=0,cycles=len(self.completed),production_execution=False))
        finally:
            (self.dest/'demo-driver.log').write_bytes(b''.join(logs))
            if hasattr(self,'gateway'):self.gateway.shutdown();self.gateway.server_close()


def main():
    p=argparse.ArgumentParser();p.add_argument('--tools',type=Path,required=True);p.add_argument('--evidence',type=Path,required=True);p.add_argument('--credentials',type=Path,required=True);p.add_argument('--stop-file',type=Path,required=True);a=p.parse_args()
    os.umask(0o077)
    h=DemoHarness(a.evidence.resolve(),a.tools.resolve(),'normal')
    h.credentials=a.credentials.resolve();h.stop_file=a.stop_file.resolve()
    def stop(*_):raise KeyboardInterrupt
    signal.signal(signal.SIGTERM,stop)
    status=1
    try:h.demo(a.tools.resolve(),900);status=0
    except KeyboardInterrupt:status=130
    finally:
        h.record('runner-exit',dict(exit_code=status,production_execution=False,teardown_is_not_physical_proof=True))
        h.containers=[cid for cid in h.containers if subprocess.run(['docker','inspect',cid],capture_output=True).returncode==0]
        h.close()
        h.credentials.unlink(missing_ok=True)
    return status

if __name__=='__main__':raise SystemExit(main())
