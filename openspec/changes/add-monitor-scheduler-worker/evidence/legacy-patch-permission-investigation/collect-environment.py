import json, subprocess, datetime
commands = [
 ['docker','inspect','--format','{{.Id}} {{.Config.Image}} privileged={{.HostConfig.Privileged}} caps={{json .HostConfig.CapAdd}} ports={{json .HostConfig.PortBindings}} networks={{json .NetworkSettings.Networks}}','devpilot-scheduler-apply-pg18'],
 ['docker','network','inspect','--format','{{.Id}} internal={{.Internal}} ipv6={{.EnableIPv6}}','devpilot-scheduler-apply-control'],
 ['docker','image','inspect','--format','{{.Id}}','devpilot-scheduler-fixture'],
 ['go','version'],
]
out = {'collected_at': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'commands': []}
for command in commands:
 r=subprocess.run(command, capture_output=True, text=True)
 out['commands'].append({'command':command,'exit_code':r.returncode,'stdout':r.stdout,'stderr':r.stderr})
 if r.returncode: raise RuntimeError(command)
print(json.dumps(out,indent=2))
