"""Numeric-only privilege drop, avoiding libc/NSS identity resolution.

Run Python with -I -S. The complete process, including this setup, is traced.
Do not add socket exceptions to compensate for a wrapper's NSS traffic.
"""
import ctypes
import os
import sys

libc=ctypes.CDLL(None,use_errno=True)
for cap in range(41):
    if libc.prctl(24,cap,0,0,0)!=0:
        raise OSError(ctypes.get_errno(),'drop capability bounding set')
if libc.prctl(38,1,0,0,0)!=0:
    raise OSError(ctypes.get_errno(),'set no_new_privs')
os.setgroups([])
os.setgid(65534)
os.setuid(65534)
case=sys.argv[1]
env={'PATH':'/usr/bin:/bin','EXECUTION_ISOLATED':'1','EXEC_CASE':case,
     'HTTP_PROXY':'http://127.0.0.54:8080','HTTPS_PROXY':'http://127.0.0.54:8080',
     'ALL_PROXY':'socks5://127.0.0.54:8080','http_proxy':'http://127.0.0.54:8080',
     'https_proxy':'http://127.0.0.54:8080','all_proxy':'socks5://127.0.0.54:8080',
     'NO_PROXY':'','no_proxy':''}
os.execve('/toolbin/execution.test',['/toolbin/execution.test','-test.run','^TestExecutionProbe$','-test.v','-test.timeout=45s'],env)
