"""Embedded, isolated Python helper entered with nsenter, not Docker exec."""
import base64
import ctypes
import json
import os
from pathlib import Path
import resource
import subprocess
import sys

expected, interfaces, tables = sys.argv[1], json.loads(sys.argv[2]), json.loads(sys.argv[3])
if os.getuid() != 0 or os.readlink('/proc/self/ns/net') != expected:
    raise SystemExit(111)  # PID reuse / wrong namespace: mutate nothing.
resource.setrlimit(resource.RLIMIT_AS, (64*1024*1024, 64*1024*1024))
resource.setrlimit(resource.RLIMIT_NOFILE, (32, 32))
resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
os.umask(0o077)
libc = ctypes.CDLL(None, use_errno=True)
for cap in range(int(Path('/proc/sys/kernel/cap_last_cap').read_text())+1):
    if cap != 12 and libc.prctl(24, cap, 0, 0, 0) != 0:
        raise SystemExit(112)
class Header(ctypes.Structure):
    _fields_ = [('version', ctypes.c_uint32), ('pid', ctypes.c_int)]
class Data(ctypes.Structure):
    _fields_ = [('effective', ctypes.c_uint32), ('permitted', ctypes.c_uint32), ('inheritable', ctypes.c_uint32)]
header, caps = Header(0x20080522, 0), (Data*2)()
caps[0].effective = caps[0].permitted = 1 << 12
if libc.capset(ctypes.byref(header), ctypes.byref(caps)) != 0 or libc.prctl(38, 1, 0, 0, 0) != 0:
    raise SystemExit(113)
status = dict(line.split(':', 1) for line in Path('/proc/self/status').read_text().splitlines() if ':' in line)
if any(int(status[k].strip(), 16) != 1 << 12 for k in ('CapEff', 'CapPrm', 'CapBnd')) or int(status['CapAmb'].strip(), 16) != 0 or status['NoNewPrivs'].strip() != '1':
    raise SystemExit(114)
# SYS_ADMIN/SETPCAP are gone. The process cannot enter another namespace.
print(json.dumps(dict(ready=True, namespace=expected, capability=1 << 12)), flush=True)

def invoke(args, data=b''):
    link_query = args == ['ip', '-j', 'link', 'show']
    if not 1 <= len(args) <= 64 or (args[0] not in ('nft', 'tc') and not link_query) or any(not isinstance(v, str) or len(v) > 256 for v in args):
        raise ValueError('command')
    p = subprocess.run(['/usr/sbin/'+args[0], *args[1:]], input=data, capture_output=True, timeout=2)
    if p.returncode or len(p.stdout) > 131072:
        raise RuntimeError('command rejected')
    return p.stdout

try:
    while True:
        line = sys.stdin.buffer.readline(131073)
        if not line:
            break
        if len(line) > 131072 or not line.endswith(b'\n'):
            raise RuntimeError('frame')
        request = json.loads(line)
        data = base64.b64decode(request['data'], validate=True)
        if len(data) > 65536:
            raise RuntimeError('data')
        try:
            output = invoke(request['args'], data)
            result = dict(ok=True, output=base64.b64encode(output).decode())
        except (ValueError, RuntimeError, subprocess.TimeoutExpired):
            result = dict(ok=False)
        print(json.dumps(result), flush=True)
finally:
    # EOF is a network revocation only, never physical release or SQL settlement.
    # Default drop precedes clearing nft grants. Only fresh owned veths/tables.
    for iface in interfaces:
        try: invoke(['tc', 'filter', 'replace', 'dev', iface, 'egress', 'pref', '1', 'protocol', 'all', 'matchall', 'action', 'drop'])
        except Exception: pass
    for table in tables:
        try: invoke(['nft', '-f', '-'], ('flush set inet '+table+' allow\n').encode())
        except Exception: pass
