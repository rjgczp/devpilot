import subprocess,json,hashlib,gzip,datetime,pathlib
p=pathlib.Path(__file__).parent
sql="SELECT json_build_object('database',current_database(),'version',current_setting('server_version'),'migration',(SELECT coalesce(max(version_id),0) FROM goose_db_version WHERE is_applied),'schedules',to_regclass('public.monitor_schedules'),'temporary_roles',(SELECT count(*) FROM pg_roles WHERE rolname LIKE 'checkpoint1_%' OR rolname LIKE 'scheduler_test_%' OR rolname LIKE 'scheduler_claim_%'),'other_active_transactions',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND xact_start IS NOT NULL),'lock_waiters',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'));"
cmd=['docker','exec','--user','postgres','devpilot-scheduler-apply-pg18','psql','-X','-U','devpilot_fixture','-d','devpilot_registration_test','-At','-v','ON_ERROR_STOP=1','-c',sql]
r=subprocess.run(cmd,capture_output=True,text=True,check=True)
state=json.loads(r.stdout);assert state['database']=='devpilot_registration_test' and state['version'].startswith('18.') and state['migration']==0 and state['schedules'] is None and state['temporary_roles']==state['other_active_transactions']==state['lock_waiters']==0
(p/'final-postflight.json').write_text(json.dumps({'command':cmd,'exit_code':r.returncode,'state':state},indent=2)+'\n')
cmd=['docker','logs','--since','2026-10-20T08:20:00Z','--timestamps','devpilot-scheduler-apply-pg18']
r=subprocess.run(cmd,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,check=True)
(p/'postgres-raw.log.gz').write_bytes(gzip.compress(r.stdout,mtime=0))
(p/'postgres-log-provenance.json').write_text(json.dumps({'command':cmd,'exit_code':r.returncode,'uncompressed_bytes':len(r.stdout),'uncompressed_sha256':hashlib.sha256(r.stdout).hexdigest(),'collected_at':datetime.datetime.now(datetime.timezone.utc).isoformat()},indent=2)+'\n')
print(json.dumps(state));print('server log bytes',len(r.stdout))
