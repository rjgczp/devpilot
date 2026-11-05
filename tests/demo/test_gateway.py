"""Small refusal checks for the new local-only browser ingress."""
import http.client
from pathlib import Path
import sys
import unittest
from unittest.mock import patch
sys.path.insert(0,str(Path(__file__).resolve().parent))
from gateway import serve

class GatewayBoundary(unittest.TestCase):
 def test_rejects_host_path_and_oversized_body_before_docker(self):
  server=serve('fixture-not-started')
  try:
   with patch('gateway.subprocess.run') as run:
    for path,headers,want in [('/api/v1/auth/me',{'Host':'evil.example'},400),('http://example.com/',{'Host':'127.0.0.1:5174'},400),('/@fs/private',{'Host':'127.0.0.1:5174'},400),('/api/v1/auth/login',{'Host':'127.0.0.1:5174','Content-Length':'16385'},413)]:
     client=http.client.HTTPConnection('::1',5174,timeout=2)
     client.request('GET',path,headers=headers)
     response=client.getresponse();self.assertEqual(response.status,want);response.read();client.close()
    run.assert_not_called()
  finally:server.shutdown();server.server_close()

if __name__=='__main__':unittest.main()
