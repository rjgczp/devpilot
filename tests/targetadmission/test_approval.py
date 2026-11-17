import datetime,importlib.util,json,pathlib,sys,tempfile,unittest
ROOT=pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'runtime/private'))
import approval
class Approval(unittest.TestCase):
 def approved(self):
  n=approval.now();return dict(workspace_id='w',project_id='p',monitor_id='m',configuration_version='1',url='https://monitor.owned-test.example.com/check',addresses=['93.184.216.34'],dns_digest='a'*64,approved_at=(n-datetime.timedelta(seconds=1)).isoformat(),expires_at=(n+datetime.timedelta(minutes=5)).isoformat())
 def test_scoped_expiring_independent_network_check(self):
  a=self.approved();line={k:a[k] for k in ('workspace_id','project_id','monitor_id','configuration_version')}
  r=dict(host='monitor.owned-test.example.com',scheme='https',port=443,ip=a['addresses'][0],dns_digest=a['dns_digest'],deadline=(approval.now()+datetime.timedelta(seconds=10)).isoformat())
  self.assertTrue(approval.permits(a,line,r))
  for field,value in [('dns_digest','b'*64),('ip','10.0.0.1'),('host','other.example.com'),('port',80),('deadline',(approval.now()+datetime.timedelta(hours=1)).isoformat())]:
   self.assertFalse(approval.permits(a,line,dict(r,**{field:value})))
  self.assertFalse(approval.permits(a,dict(line,configuration_version='2'),r))
  a['expires_at']=(approval.now()-datetime.timedelta(seconds=1)).isoformat();self.assertFalse(approval.permits(a,line,r))
 def test_owned_receiver_source_parses(self):
  import ast
  spec=importlib.util.spec_from_file_location('approved_https',ROOT/'tests/targetadmission/https.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
  h=object.__new__(module.ApprovedHarness);h.case='normal';h.mode='normal'
  source=h.receiver_source();ast.parse(source.decode());self.assertIn(b'monitor.owned-test.example.com',source);self.assertIn(b'edge.owned-test.example.com',source)
 def test_control_refusal_requires_original_physical_acceptance_before_controller(self):
  import launcher,threading
  with tempfile.TemporaryDirectory() as d:
   state=pathlib.Path(d);run='01900000-0000-7000-8000-000000000005';line=dict(run_id=run)
   r=object.__new__(launcher.Runtime);r.state=state;r.done=[];r.closing=False;r.ready=set();r.children={}
   r.recover=lambda *args:self.fail('unverified release reached Controller')
   (state/(run+'.owner.json')).write_text(json.dumps(dict(index=0,line=line)))
   with self.assertRaises(AssertionError):r.route('worker0',dict(event=dict(event='control_result_committed',run_id=run)))
   (state/(run+'.database-accepted')).write_text('trusted physical proof was accepted')
   calls=[];r.recover=lambda *args:calls.append(args);r.status=lambda *args,**kw:None
   r.route('worker0',dict(event=dict(event='control_result_committed',run_id=run)))
   self.assertEqual(calls,[(0,line,True)]);self.assertEqual(r.done,[])
 def test_operator_file_cannot_be_group_writable(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'approval';p.write_text(json.dumps(self.approved()));p.chmod(0o644)
   self.assertEqual(approval.load_approval(str(p),'monitor.owned-test.example.com','93.184.216.34')['monitor_id'],'m')
   p.chmod(0o664)
   with self.assertRaises(AssertionError):approval.load_approval(str(p),'monitor.owned-test.example.com','93.184.216.34')
if __name__=='__main__':unittest.main()
