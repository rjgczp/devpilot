"""Loopback-only, bounded browser ingress via Docker exec private stdio.

No ports/routes are added to the internal control network. The in-container
client has exactly one destination: its own loopback API, never a user URL.
Cookies and bodies stay in private pipes and are never logged.
"""
import base64
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import socket
import subprocess
import threading

CLIENT = r'''
import base64,http.client,json,sys
r=json.load(sys.stdin)
assert r['path'].startswith('/api/v1/') and not r['path'].startswith('//')
c=http.client.HTTPConnection('127.0.0.1',5174,timeout=5)
c.request(r['method'],r['path'],body=base64.b64decode(r['body']),headers=r['headers'])
x=c.getresponse();b=x.read(2*1024*1024+1);assert len(b)<=2*1024*1024
print(json.dumps(dict(status=x.status,headers=x.getheaders(),body=base64.b64encode(b).decode())))
c.close()
'''


def serve(container):
    slots = threading.BoundedSemaphore(4)
    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *_):
            pass
        def proxy(self):
            self.connection.settimeout(8)
            if self.headers.get('Host') != '127.0.0.1:5174' or not self.path.startswith('/api/v1/') or self.path.startswith('//'):
                self.send_error(400); return
            if not slots.acquire(blocking=False):
                self.send_error(503); return
            try:
                length = int(self.headers.get('Content-Length', '0'))
                if length < 0 or length > 16384 or self.headers.get('Transfer-Encoding'):
                    self.send_error(413); return
                headers = {k: v for k, v in self.headers.items() if k.lower() in ('host','origin','cookie','content-type','accept','if-match','if-none-match','x-devpilot-csrf')}
                request = dict(method=self.command,path=self.path,headers=headers,body=base64.b64encode(self.rfile.read(length)).decode())
                result = subprocess.run(['docker','exec','-i',container,'python3','-c',CLIENT],input=json.dumps(request).encode(),stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,timeout=7)
                if result.returncode:
                    self.send_error(503); return
                response = json.loads(result.stdout)
                body = base64.b64decode(response['body'],validate=True)
                self.send_response(response['status'])
                for k,v in response['headers']:
                    if k.lower() in ('content-type','cache-control','x-content-type-options','set-cookie','etag','devpilot-activation-projection','allow'):
                        self.send_header(k,v)
                self.send_header('Content-Length',str(len(body))); self.end_headers()
                self.wfile.write(body)
            except (BrokenPipeError,ConnectionResetError):
                pass
            except (OSError,ValueError,subprocess.TimeoutExpired):
                self.send_error(503)
            finally:
                slots.release()
        do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = do_HEAD = proxy
    class Server(ThreadingHTTPServer):
        address_family = socket.AF_INET6
        daemon_threads = True
    server = Server(('::1',5174), Handler)
    threading.Thread(target=server.serve_forever,daemon=True).start()
    return server
