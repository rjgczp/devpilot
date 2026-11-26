"""Bounded host-policy executor, pinned before actors start, without Docker RPC.

Only the trusted allocator owns its anonymous pipes. It never signs a receipt.
"""
import base64
import json
import os
from pathlib import Path
import re
import select
import subprocess
import threading
import time


class NamespaceCommandError(RuntimeError):
    pass


class NamespaceExecutor:
    def __init__(self, pid, namespace, interfaces, tables):
        if not isinstance(pid, int) or pid <= 1 or not re.fullmatch(r'net:\[\d+\]', namespace):
            raise ValueError('bound namespace required')
        if namespace == os.readlink('/proc/self/ns/net'):
            raise ValueError('real host namespace is not a prototype target')
        if len(interfaces) != 2 or len(set(interfaces)) != 2 or any(not re.fullmatch('[a-zA-Z0-9_-]{1,15}', v) for v in interfaces):
            raise ValueError('owned veth pair required')
        if len(tables) != 2 or any(not re.fullmatch('dpl_(policy|guard)_[0-9a-f]{12}', v) for v in tables):
            raise ValueError('owned policy required')
        source = Path(__file__).with_name('namespace_child.py').read_text()
        command = ([] if os.geteuid() == 0 else ['sudo', '-n']) + [
            '/usr/bin/nsenter', '--net=/proc/'+str(pid)+'/ns/net', '--',
            '/usr/bin/python3', '-I', '-S', '-B', '-u', '-c', source,
            namespace, json.dumps(interfaces), json.dumps(tables)]
        self.lock = threading.Lock()
        self.closed = False
        self.buffer = bytearray()
        self.process = subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, close_fds=True, bufsize=0)
        try:
            ready = self._read()
            if ready != dict(ready=True, namespace=namespace, capability=1 << 12):
                raise RuntimeError('namespace executor did not attest')
        except BaseException:
            self.close()
            raise

    def _read(self):
        end = time.monotonic()+5
        while len(self.buffer) <= 262144:
            if b'\n' in self.buffer:
                raw, _, rest = self.buffer.partition(b'\n')
                self.buffer = bytearray(rest)
                return json.loads(raw)
            if not select.select([self.process.stdout], [], [], max(0, end-time.monotonic()))[0]:
                raise RuntimeError('namespace executor deadline')
            raw = os.read(self.process.stdout.fileno(), 16384)
            if not raw:
                raise RuntimeError('namespace executor lost')
            self.buffer += raw
        raise RuntimeError('namespace executor frame')

    def run(self, *args, data=None):
        with self.lock:
            if self.closed:
                raise RuntimeError('namespace executor closed')
            payload = json.dumps(dict(args=args, data=base64.b64encode(data or b'').decode())).encode()+b'\n'
            if len(payload) > 131072:
                raise ValueError('namespace command bound')
            try:
                self.process.stdin.write(payload)
                self.process.stdin.flush()
                response = self._read()
                if response.get('ok') is not True:
                    raise NamespaceCommandError('trusted namespace command rejected')
                return base64.b64decode(response['output'], validate=True)
            except NamespaceCommandError:
                raise
            except BaseException:
                # A desynchronized channel is never reused. EOF causes the
                # independently running helper to revoke, even with Docker down.
                self.closed = True
                self.process.stdin.close()
                raise

    def close(self):
        with self.lock:
            self.closed = True
            if not self.process.stdin.closed:
                self.process.stdin.close()
        code = self.process.wait(timeout=12)
        if code != 0:
            raise RuntimeError('namespace executor abnormal exit')
