"""Security regressions run unchanged in normal, -O and env optimization modes.

No Docker, PG, network or actual process termination. Fakes must never reach an
allocation/signing/release side effect on rejected input. unittest remains active
when Python removes language Assert statements.
"""
import ast,datetime,importlib.util,json,os,pathlib,runpy,sys,tempfile,types,unittest
from unittest import mock
ROOT=pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0,str(ROOT/'runtime/private'))
import approval,launcher
from kernel import Kernel,project_event
from assets.authority import Authority

def lineage():
 keys=['workspace_id','project_id','monitor_id','run_id','namespace_incarnation','supervisor_incarnation']
 return dict({k:'00000000-0000-4000-8000-'+str(i+1).zfill(12) for i,k in enumerate(keys)},configuration_version='1',activation_generation='1',scheduler_epoch='1',fencing_token='1')
def approved():
 n=approval.now();line=lineage()
 return dict({k:line[k] for k in ('workspace_id','project_id','monitor_id','configuration_version')},url='https://monitor.owned-test.example.com/check',addresses=['93.184.216.34'],dns_digest='a'*64,approved_at=(n-datetime.timedelta(seconds=1)).isoformat(),expires_at=(n+datetime.timedelta(minutes=5)).isoformat())
def definitions(name):
 source=(ROOT/'runtime/private/assets'/name).read_text();tree=ast.parse(source)
 return [n for n in tree.body if isinstance(n,ast.FunctionDef)]

class OptimizedChecks(unittest.TestCase):
 def test_approval_rejects_unapproved_expired_dns_and_private_targets(self):
  with tempfile.TemporaryDirectory() as d:
   p=pathlib.Path(d)/'approval';a=approved();p.write_text(json.dumps(a));p.chmod(0o600)
   self.assertEqual(approval.load_approval(str(p),'monitor.owned-test.example.com','93.184.216.34'),a)
   for host,ip,changes in [
    ('other.example.com','93.184.216.34',{}),
    ('monitor.owned-test.example.com','93.184.216.35',{}),
    ('monitor.owned-test.example.com','93.184.216.34',dict(expires_at=(approval.now()-datetime.timedelta(seconds=1)).isoformat())),
    ('monitor.owned-test.example.com','93.184.216.34',dict(dns_digest='invalid')),
    ('monitor.owned-test.example.com','169.254.169.254',dict(addresses=['169.254.169.254'])),
    ('monitor.owned-test.example.com','fd00::1',dict(addresses=['fd00::1'])),
   ]:
    with self.subTest(host=host,ip=ip,changes=changes):
     p.write_text(json.dumps(dict(a,**changes)))
     with self.assertRaises(RuntimeError):approval.load_approval(str(p),host,ip)
   p.write_text(json.dumps(a));p.chmod(0o666)
   with self.assertRaises(RuntimeError):approval.load_approval(str(p),'monitor.owned-test.example.com','93.184.216.34')
 def test_wrong_permit_and_dns_binding_never_reach_network_installer(self):
  line=lineage();a=approved();r=dict(line,host='monitor.owned-test.example.com',scheme='https',port=443,ip='93.184.216.34',dns_digest='a'*64,deadline=(approval.now()+datetime.timedelta(seconds=10)).isoformat())
  k=object.__new__(Kernel);k.approval=a;k.active={0:dict(line=line)};k.send=mock.Mock(side_effect=AssertionError('network installer reached'))
  permit=dict(permit_id='fixed',committed_at='fixed',permit_deadline='fixed')
  self.assertTrue(approval.permits(a,line,r))
  for changes in [dict(dns_digest='b'*64),dict(ip='93.184.216.35'),dict(fencing_token='2'),dict(deadline=(approval.now()-datetime.timedelta(seconds=1)).isoformat())]:
   with self.subTest(changes=changes):
    with self.assertRaises(RuntimeError):k._call(0,dict(op='install',line=line,request=dict(r,**changes),permit=permit))
  k.send.assert_not_called()
  k.authority=mock.Mock();foreign=dict(line,configuration_version='2')
  with self.assertRaises(RuntimeError):k.prepare(0,foreign)
  k.active={}
  with self.assertRaises(RuntimeError):k.prepare(0,foreign)
  k.authority.register.assert_not_called()
 def test_untrusted_recovery_and_stop_proof_cannot_create_release_markers(self):
  with tempfile.TemporaryDirectory() as d:
   state=pathlib.Path(d);line=lineage();k=object.__new__(Kernel);k.state=state
   for op in ('accepted','recovered'):
    with self.assertRaises(RuntimeError):k._call(0,dict(op=op,line=line))
   self.assertEqual(list(state.iterdir()),[])
   signer=object.__new__(Authority);signer.path=state;signer.pending={line['run_id']:line};signer.key_id='a'*64
   network=[dict(role=role,lineage=line,namespace='net:['+str(i+1)+']',link_removed=True,conntrack_empty=True,stopped_at=approval.now().isoformat()) for i,role in enumerate(('run','gateway'))]
   process=dict(lineage=line,remaining_pids=[123],launch_consumed=True,pids=[dict(pidfd_exited=True)],net_namespace='net:[1]',pid_namespace='pid:[1]',at_ns=int(approval.now().timestamp()*1e9))
   retired=dict(event='pid_namespace_retiring',lineage=line,pid_namespace='pid:[1]')
   with mock.patch('assets.authority.subprocess.run') as command:
    with self.assertRaises(RuntimeError):signer.sign(line,network,process,retired)
    command.assert_not_called()
   self.assertEqual(list(state.iterdir()),[])
 def test_container_uid_and_sealed_identity_are_enforced(self):
  k=object.__new__(Kernel);k.target='owned';k.inspect=lambda _:dict(HostConfig=dict(NetworkMode='host',PortBindings={},Privileged=False))
  with self.assertRaises(RuntimeError):k.validate_target()
  proof=dict(tcp_only=True,sockets_denied=False,fd_limit=256,no_new_privileges=True,capabilities_zero=False,secrets_unavailable=True,uid=65534,gid=65534)
  with self.assertRaises(RuntimeError):project_event(dict(event='sealed',proof=proof),lineage())
  with mock.patch.object(sys,'argv',['control_child','worker','own-inc','private-approved']),mock.patch.object(os,'getuid',return_value=65534),mock.patch.object(os,'pipe',side_effect=AssertionError('IPC allocated before UID check')):
   with self.assertRaises(RuntimeError):runpy.run_path(str(ROOT/'runtime/private/control_child.py'),run_name='negative_control')
  with mock.patch.object(os,'getuid',return_value=0),mock.patch('builtins.open',side_effect=AssertionError('Probe identity check skipped')):
   with self.assertRaises(RuntimeError):runpy.run_path(str(ROOT/'runtime/private/assets/probe_bootstrap.py'),run_name='negative_probe')
 def test_real_stop_predicate_and_prctl_calls_survive_compilation_optimization(self):
  # Execute actual definitions, not a reimplementation of the stop predicate.
  ns={};tree=ast.Module(body=definitions('termination_core.py'),type_ignores=[])
  exec(compile(tree,'termination_core.py','exec'),ns)
  ns.update(reap=mock.Mock(),pids=mock.Mock(side_effect=[[],[123]]),time=__import__('time'),proof=None)
  with self.assertRaises(RuntimeError):ns['stop']()
  self.assertIsNone(ns['proof']);self.assertEqual(ns['pids'].call_count,2)
  tree=ast.parse((ROOT/'runtime/private/assets/lifecycle.py').read_text())
  guard=next(n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='require')
  call=next(n for n in tree.body if isinstance(n,ast.Expr) and isinstance(n.value,ast.Call) and isinstance(n.value.func,ast.Name) and n.value.func.id=='require' and any(isinstance(v,ast.Attribute) and v.attr=='prctl' for v in ast.walk(n)))
  ns={'libc':types.SimpleNamespace(prctl=mock.Mock(return_value=0))}
  code=compile(ast.Module(body=[guard,call],type_ignores=[]),'lifecycle.py','exec')
  exec(code,ns);self.assertEqual(ns['libc'].prctl.call_args_list,[mock.call(4,0,0,0,0),mock.call(36,1,0,0,0)])
  ns['libc'].prctl=mock.Mock(return_value=-1)
  with self.assertRaises(RuntimeError):exec(code,ns)
  self.assertEqual(ns['libc'].prctl.call_count,1)
 def test_runtime_has_no_removable_assert_and_exact_explicit_guard(self):
  guard=ast.parse("def require(condition, message='private boundary rejected'):\n if not condition:raise RuntimeError(message)\n").body[0]
  for p in (ROOT/'runtime/private').rglob('*.py'):
   tree=ast.parse(p.read_text());self.assertFalse(any(isinstance(n,ast.Assert) for n in ast.walk(tree)),str(p))
   helpers=[n for n in tree.body if isinstance(n,ast.FunctionDef) and n.name=='require']
   used=any(isinstance(n,ast.Call) and isinstance(n.func,ast.Name) and n.func.id=='require' for n in ast.walk(tree))
   self.assertEqual(len(helpers),1 if used else 0,str(p))
   if used:self.assertEqual(ast.dump(helpers[0]),ast.dump(guard),str(p))
 def test_optimized_launcher_and_unverified_journal_never_start_services(self):
  with tempfile.TemporaryDirectory() as d:
   state=pathlib.Path(d);line=lineage();(state/(line['run_id']+'.owner.json')).write_text(json.dumps(dict(index=0,line=line)))
   with mock.patch.object(sys,'argv',['launcher','run','--run-approved','--config','unused','--state',str(state)]),mock.patch.object(sys,'platform','linux'),mock.patch.object(launcher,'load',return_value=dict(mode='private-approved')),mock.patch.object(launcher,'Runtime') as runtime,mock.patch.object(launcher,'open',create=True),mock.patch.object(launcher.fcntl,'flock'):
    with self.assertRaises(RuntimeError):launcher.main()
    runtime.assert_not_called()
   self.assertFalse((state/(line['run_id']+'.database-accepted')).exists())

if __name__=='__main__':
 print(json.dumps(dict(optimization=sys.flags.optimize,no_site=sys.flags.no_site)),flush=True)
 unittest.main(verbosity=2)
