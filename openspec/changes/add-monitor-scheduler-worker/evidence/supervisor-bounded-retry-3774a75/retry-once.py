import datetime, hashlib, json, os, pathlib, signal, subprocess, sys, threading, time

repo = pathlib.Path('/Users/sansea/Documents/ChatGPT/devpilot')
evidence = pathlib.Path('/private/tmp/devpilot-supervisor-bounded-retry-3774a75')
evidence.mkdir(exist_ok=True)
# An exclusive sentinel permits exactly one real invocation under this request.
sentinel = evidence / 'attempt.once'
with sentinel.open('x') as f:
    f.write(datetime.datetime.now(datetime.timezone.utc).isoformat()+'\n')
pg = 'devpilot-scheduler-apply-pg18'
job = 'devpilot-scheduler-supervisor-retry-3774a75'
expected = 'db0774ff4cff96278db0eeab88ec0c2ed64f561a1c28d1671c200e4f31015103'
head = subprocess.check_output(['git','rev-parse','HEAD'],cwd=repo,text=True).strip()
assert head == '3774a75eb7d3e733b174530d71816ed319d9f084'
assert not subprocess.check_output(['git','status','--porcelain'],cwd=repo)
def capture(argv, timeout=10):
    p = subprocess.run(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True,timeout=timeout)
    if p.returncode:
        raise RuntimeError('read-only preflight failed: '+p.stderr[:400])
    return p.stdout
info = json.loads(capture(['docker','inspect',pg]))[0]
assert info['Id'] == expected and info['State']['Running']
assert not info['HostConfig']['Privileged'] and not info['HostConfig'].get('CapAdd')
assert not info['HostConfig'].get('PortBindings')
assert info['Image']=='sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336'
network=json.loads(capture(['docker','network','inspect','devpilot-scheduler-apply-control']))[0]
assert network['Internal'] and set(info['NetworkSettings']['Networks'])=={'devpilot-scheduler-apply-control'}
image=json.loads(capture(['docker','image','inspect','devpilot-scheduler-fixture']))[0]
assert image['Id']=='sha256:60a78b61cd1378bdb343af6bf6321f383a33f64737c54852a2d462cdfc1df15c'
base=['docker','exec','--user','postgres',pg,'psql','-X','-U','devpilot_fixture','-d','devpilot_registration_test','-A','-t','-v','ON_ERROR_STOP=1','-c']
preflight=capture(base+["SELECT json_build_object('database',current_database(),'server_version_num',current_setting('server_version_num'),'session_user',session_user,'logging_collector',current_setting('logging_collector'),'log_min_error_statement',current_setting('log_min_error_statement'),'lock_timeout',current_setting('lock_timeout'),'statement_timeout',current_setting('statement_timeout'),'transaction_timeout',current_setting('transaction_timeout'),'prepared_transactions',current_setting('max_prepared_transactions'))"])
identity=json.loads(preflight)
assert identity['database']=='devpilot_registration_test' and 180000<=int(identity['server_version_num'])<190000
assert int(capture(base+["SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state<>'idle'"]).strip())==0
assert int(capture(base+["SELECT count(*) FROM goose_db_version WHERE is_applied AND version_id>0"]).strip())==0
(evidence/'preflight.json').write_text(json.dumps({'head':head,'pg_id':info['Id'],'pg_image':info['Image'],'privileged':info['HostConfig']['Privileged'],'cap_add':info['HostConfig'].get('CapAdd'),'port_bindings':info['HostConfig'].get('PortBindings'),'networks':list(info['NetworkSettings']['Networks']),'network_internal':network['Internal'],'fixture_image':image['Id'],'database':identity,'other_active_backends':0,'applied_positive_migrations':0},indent=2)+'\n')
started=datetime.datetime.now(datetime.timezone.utc).isoformat()
logfile=(evidence/'postgres-supervisor.log').open('wb')
logproc=subprocess.Popen(['docker','logs','--follow','--timestamps','--since',started,pg],stdout=logfile,stderr=subprocess.STDOUT)
stop=threading.Event()
query="""SELECT json_build_object('observed_at',clock_timestamp(),'backends',(SELECT coalesce(json_agg(json_build_object('pid',a.pid,'user',a.usename,'state',a.state,'xact_start',a.xact_start,'query_start',a.query_start,'wait_event_type',a.wait_event_type,'wait_event',a.wait_event,'blocking_pids',pg_blocking_pids(a.pid),'operation',CASE WHEN strpos(a.query,'prepare_run')>0 THEN 'prepare_run' WHEN strpos(a.query,'consume_input')>0 THEN 'consume_input' WHEN strpos(a.query,'start_run')>0 THEN 'start_run' WHEN strpos(a.query,'scheduler_private.claim')>0 THEN 'claim' ELSE 'setup_cleanup_or_other' END,'locks',(SELECT coalesce(json_agg(json_build_object('locktype',l.locktype,'relation',l.relation::regclass::text,'mode',l.mode,'granted',l.granted,'transactionid',l.transactionid::text)),'[]'::json) FROM pg_locks l WHERE l.pid=a.pid))),'[]'::json) FROM pg_stat_activity a WHERE a.datname=current_database() AND a.pid<>pg_backend_pid()))"""
def observe():
    with (evidence/'pg-locks.jsonl').open('w') as out:
        while not stop.is_set():
            try:
                value=capture(base+[query],timeout=2)
                json.loads(value)
                out.write(value.strip()+'\n');out.flush()
            except Exception as exc:
                out.write(json.dumps({'observer_error':str(exc)[:400],'at':datetime.datetime.now(datetime.timezone.utc).isoformat()})+'\n');out.flush()
            stop.wait(0.05)
observer=threading.Thread(target=observe)
observer.start()
command=['docker','run','--rm','--name',job,'--network','container:'+pg,'-v','/private/tmp/devpilot-scheduler-tools:/toolbin:ro','-v',str(repo)+':'+str(repo)+':ro','-e','PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin','-e','TEST_DATABASE_URL=postgres://devpilot_fixture:devpilot_fixture_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable','--entrypoint','/toolbin/integration-supervisor-retry.test','devpilot-scheduler-fixture','-test.run','^TestSchedulerSupervisorOwnsOneInputCapability$','-test.count=1','-test.timeout=90s','-test.v']
recorder=['python3',str(repo/'tests/scheduler/record.py'),'--evidence',str(evidence),'--name','pg-input-once','--binary','/private/tmp/devpilot-scheduler-tools/integration-supervisor-retry.test','--']+command
begin=time.monotonic();timed_out=False
with (evidence/'recorder.log').open('wb') as output:
    proc=subprocess.Popen(recorder,cwd=repo,stdout=output,stderr=subprocess.STDOUT,start_new_session=True)
    try:
        code=proc.wait(timeout=95)
    except subprocess.TimeoutExpired:
        timed_out=True;code=124
        with (evidence/'timeout-stop.log').open('wb') as out:
            subprocess.run(['docker','kill',job],stdout=out,stderr=subprocess.STDOUT,timeout=5)
        os.killpg(proc.pid,signal.SIGTERM)
        proc.wait(timeout=5)
stop.set();observer.join(timeout=3)
logproc.terminate()
try:logproc.wait(timeout=3)
except subprocess.TimeoutExpired:logproc.kill();logproc.wait(timeout=2)
logfile.close()
post=capture(base+["SELECT json_build_object('database_time',clock_timestamp(),'applied_versions',(SELECT coalesce(json_agg(version_id ORDER BY id),'[]'::json) FROM goose_db_version WHERE is_applied),'scheduler_tables',(SELECT coalesce(json_agg(tablename),'[]'::json) FROM pg_tables WHERE schemaname='public' AND tablename IN ('monitor_runs','monitor_schedules','scheduler_control')),'temporary_runtime_roles',(SELECT coalesce(json_agg(rolname),'[]'::json) FROM pg_roles WHERE rolname IN ('scheduler_claim_worker1','scheduler_claim_supervisor')),'active_transactions',(SELECT coalesce(json_agg(json_build_object('pid',pid,'user',usename,'wait_event_type',wait_event_type,'wait_event',wait_event,'blocking_pids',pg_blocking_pids(pid))),'[]'::json) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND xact_start IS NOT NULL))"])
(evidence/'postflight.json').write_text(post.strip()+'\n')
files={p.name:hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(evidence.iterdir()) if p.is_file()}
manifest={'head':head,'started_at':started,'elapsed_seconds':time.monotonic()-begin,'test':'TestSchedulerSupervisorOwnsOneInputCapability','attempt_count':1,'command':command,'exit_code':code,'outer_timeout':timed_out,'test_timeout_seconds':90,'outer_timeout_seconds':95,'unchanged_runtime_limits':{'statement_timeout':'2s','transaction_timeout':'2s','lock_timeout':'250ms','goose_cli_timeout':'30s','goose_context_timeout':'40s'},'supervisor_log_scope':'PostgreSQL errors and sampled sessions for real scheduler_claim_supervisor LOGIN; this database-only test has no Linux Supervisor process or Probe','files_sha256':files}
(evidence/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps({k:v for k,v in manifest.items() if k not in ('command','files_sha256')},indent=2))
sys.exit(code)
