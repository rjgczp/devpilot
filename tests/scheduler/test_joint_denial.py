"""Fixture transport regression; real anonymous pipes, no network or fake permit.

Only transport behavior is tested here. Go Client validation, actual zero TCP
and independent physical release still require the complete Linux/PG matrix.
"""
import ast,base64,json,os,unittest
from pathlib import Path
from joint_fixture import permit_command,redact_joint_line


def relay_delivery():
 # Execute the actual embedded relay function, without launching its top-level
 # process. This avoids a test copy of its EOF or descriptor-close behavior.
 tree=ast.parse(Path(__file__).with_name('joint_bootstrap.py').read_text())
 fn=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='deliver_permit')
 ns={'os':os,'base64':base64}
 exec(compile(ast.Module(body=[fn],type_ignores=[]),'<actual relay deliver_permit>','exec'),ns)
 return ns['deliver_permit']


class JointDenialTransport(unittest.TestCase):
 def test_explicit_null_keeps_original_ipc_and_closes_without_bytes(self):
  raw=b'NETWORK64 {"op":"reply","payload":{"frame":null}}\n'
  self.assertEqual(redact_joint_line(raw),raw)
  cmd=permit_command(json.loads(raw[len(b'NETWORK64 '):])['payload'])
  self.assertEqual(cmd,{'op':'permit-denied'})
  r,w=os.pipe()
  try:
   self.assertEqual(relay_delivery()(cmd,w),'reply_denied')
   self.assertEqual(os.read(r,1),b'')
   with self.assertRaises(OSError):os.fstat(w)
  finally:os.close(r)

 def test_required_success_fields_cannot_be_missing_empty_or_malformed(self):
  bad=[None,{}, {'extra':None},{'frame':None,'extra':1},{'frame':''},{'frame':False},{'frame':0},{'frame':[]},{'frame':'!bad!'},{'frame':base64.b64encode(b'truncated').decode()}]
  for payload in bad:
   with self.subTest(payload=payload):
    with self.assertRaises((ValueError,TypeError)):permit_command(payload)
    raw=b'NETWORK64 '+json.dumps({'op':'reply','payload':payload}).encode()+b'\n'
    with self.assertRaises((ValueError,TypeError)):redact_joint_line(raw)

 def test_malformed_relay_commands_fail_closed(self):
  bad=[{'op':'permit-denied','frame':None},{'op':'permit-denied','frame':''},{'op':'permit-reply'},{'op':'permit-reply','frame':None},{'op':'permit-reply','frame':''},{'op':'permit-reply','frame':'bad!'}]
  for req in bad:
   with self.subTest(req=req):
    r,w=os.pipe()
    try:
     with self.assertRaises((RuntimeError,ValueError,TypeError)):relay_delivery()(req,w)
     self.assertEqual(os.read(r,1),b'')
    finally:os.close(r)

 def test_consumed_writer_cannot_be_reused_for_denial_or_allow(self):
  for req in [{'op':'permit-denied'},{'op':'permit-reply','frame':'arbitrary'}]:
   with self.assertRaisesRegex(RuntimeError,'permit_reply_consumed'):relay_delivery()(req,None)

 def test_null_is_not_allowed_for_required_start_capabilities(self):
  for op in ['launch','replay']:
   for payload in [{'envelope':None,'receipt':None},{'envelope':None},{}, {'frame':None}]:
    with self.assertRaises((ValueError,TypeError)):redact_joint_line(b'NETWORK64 '+json.dumps({'op':op,'payload':payload}).encode()+b'\n')

 def test_no_capability_bytes_or_secrets_in_redacted_success(self):
  # Synthetic transport bytes are not a valid signed permission or a claim of
  # actual execution. They must remain completely redacted, just like a frame.
  marker=b'PRIVATE_URL_SECRET_SENTINEL'
  for op,payload in [('reply',{'frame':base64.b64encode(marker.ljust(2048,b'x')).decode()}),('launch',{'envelope':base64.b64encode(marker.ljust(4096,b'x')).decode(),'receipt':base64.b64encode(marker.ljust(1024,b'x')).decode()})]:
   got=redact_joint_line(b'NETWORK64 '+json.dumps({'op':op,'payload':payload}).encode()+b'\n')
   self.assertNotIn(marker,got)
   for v in payload.values():self.assertNotIn(v.encode(),got)
   self.assertTrue(got.startswith(b'NETWORK64-REDACTED '))


if __name__=='__main__':unittest.main()
