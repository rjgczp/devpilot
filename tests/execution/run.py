#!/usr/bin/env python3
"""Root harness configures only its disposable network-none container.

The executable being audited is a separate UID65534 process with zero caps and
an explicit empty environment (apart from test settings). No DB secret crosses.
"""
import json
import os
import re
from pathlib import Path
import subprocess
import sys
import time
from audit import classify

out=Path('/evidence')
assert os.geteuid()==0 and os.environ.get('EXECUTION_ISOLATED')=='1'
out.mkdir(exist_ok=True)
def run(*args):
    return subprocess.check_output(args,text=True)

out.joinpath('tool-versions.txt').write_text(run('dpkg-query','-W','iproute2','iptables','strace','python3','openssl','util-linux'))

run('ip','link','set','lo','up')
for ip in ('93.184.216.34/32','93.184.216.35/32','93.184.216.36/32','93.184.216.37/32','93.184.216.38/32','169.254.169.254/32'):
    run('ip','addr','add',ip,'dev','lo')
for ip in ('2606:4700:4700::1111/128','fd00::1/128','2001:4860:ffff::a9fe:a9fe/128'):
    run('ip','-6','addr','add',ip,'dev','lo','nodad')
routes=run('ip','-j','route','show','table','all')
routes6=run('ip','-6','-j','route','show','table','all')
assert not any(x.get('dst')=='default' for x in json.loads(routes)+json.loads(routes6))
links=json.loads(run('ip','-j','link'))
templates={'tunl0','gre0','gretap0','erspan0','ip_vti0','ip6_vti0','sit0','ip6tnl0','ip6gre0'}
# Docker Desktop exposes the kernel's inert tunnel templates even with network
# none. They must remain down, with no addresses/routes; no external NIC is
# admitted and the probe has no capability to bring a template up.
assert all(x['ifname']=='lo' or x['ifname'] in templates and 'UP' not in x['flags'] for x in links), 'unexpected interface'
addresses=json.loads(run('ip','-j','addr'))
assert all(x['ifname']=='lo' or not x.get('addr_info') for x in addresses),'address on tunnel template'
assert all(x.get('dev')=='lo' for x in json.loads(routes)+json.loads(routes6)),'external route'
out.joinpath('topology.json').write_text(json.dumps(dict(links=links,ipv4=json.loads(routes),ipv6=json.loads(routes6)),indent=2))

for fw in ('iptables','ip6tables'):
    run(fw,'-P','OUTPUT','DROP')
    run(fw,'-A','OUTPUT','-m','conntrack','--ctstate','ESTABLISHED,RELATED','-j','ACCEPT')
for ip,port in (('127.0.0.53','53'),('93.184.216.34','80'),('93.184.216.34','443'),('93.184.216.35','80'),('93.184.216.38','443')):
    run('iptables','-A','OUTPUT','-m','owner','--uid-owner','65534','-p','tcp','-d',ip,'--dport',port,'-j','ACCEPT')
run('iptables','-A','OUTPUT','-p','tcp','-d','93.184.216.37','--dport','80','-j','DROP')
run('ip6tables','-A','OUTPUT','-m','owner','--uid-owner','65534','-p','tcp','-d','2606:4700:4700::1111','--dport','80','-j','ACCEPT')
# A native-looking allocated IPv6 destination translated to an isolated ULA.
# Filter OUTPUT sees the translated destination and rejects independently.
run('ip6tables','-t','nat','-A','OUTPUT','-d','2001:4860:ffff::a9fe:a9fe','-p','tcp','--dport','80','-j','DNAT','--to-destination','[fd00::1]:80')
for fw in ('iptables','ip6tables'):
    run(fw,'-A','OUTPUT','-j','REJECT')

Path('/fixtures').mkdir(exist_ok=True)
prepare=subprocess.run(['/toolbin/execution.test','-test.run','^TestExecutionPrepareTLS$','-test.v'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
out.joinpath('prepare.txt').write_text(prepare.stdout)
assert prepare.returncode==0,prepare.stdout
fixture_log=out.joinpath('fixture.txt').open('w')
fixtures=subprocess.Popen(['python3','/runner/fixtures.py'],stdout=fixture_log,stderr=subprocess.STDOUT)
try:
    deadline=time.monotonic()+5
    while not out.joinpath('fixture-ready').exists() and time.monotonic()<deadline:
        assert fixtures.poll() is None,'fixture failed';time.sleep(.01)
    assert out.joinpath('fixture-ready').exists(),'fixture startup timeout'
    out.joinpath('listeners.txt').write_text(run('ss','-lnt'))
    if os.environ.get('EXECUTION_MODE')=='pg':
        out.joinpath('pg-ready').write_text('ready\n')
        while not out.joinpath('stop-pg').exists():
            assert fixtures.poll() is None,'fixture died'
            time.sleep(.02)
        out.joinpath('firewall-v4.txt').write_text(run('iptables-save','-c'))
        out.joinpath('firewall-v6.txt').write_text(run('ip6tables-save','-c'))
        sys.exit(0)
    cases=['ok','ipv6','cname','tls','cname-tls','expired','untrusted','wrong-name','tls-big','tls-stall','tls-stage','tcp-stage','dns-stage','dns-chain-slow',
           'redirect-301','redirect-302','redirect-307','redirect-308','status','infinite','chunked','gzip',
           'hints','header-big','flood','flood-lf','hints-big','upgrade','malformed','slow','t30','cancel','refuse','tcp-timeout',
           'mixed','mixed6','additional','reserved-alias','blocked-private','blocked-metadata','blocked-shared',
           'blocked-loopback','blocked-mapped','blocked-nat64','blocked-6to4','blocked-teredo','blocked-isatap',
           'blocked-doc','blocked-v6doc','blocked-ula','dns-nxdomain','dns-servfail','dns-empty','dns-truncated',
           'dns-id','dns-question','dns-trailing','dns-timeout','inconsistent','aliases','addresses','records',
           'oversize','total','dname','nat-app','rebinding','repeat','capacity','concurrency','addresses-boundary','records-boundary',
           'bypass-v4','bypass-v6','bypass-nat','bypass-public']
    results=[]
    for case in cases:
        command=['strace','-f','-qq','-yy','-s','256','-e','trace=process,connect,sendto,sendmsg,sendmmsg','-o',str(out/(case+'.trace')),
                 'python3','-I','-S','/runner/drop_probe.py',case]
        started=time.monotonic()
        child=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
        out.joinpath(case+'.txt').write_text(child.stdout)
        report=classify(out.joinpath(case+'.trace').read_text().splitlines())
        audit_exit=1 if report['violations'] else 0
        report.update(case=case,command=command,child_exit=child.returncode,audit_exit=audit_exit,duration=time.monotonic()-started)
        out.joinpath(case+'.json').write_text(json.dumps(report,indent=2)+'\n')
        assert child.returncode==0,case+': '+child.stdout
        if case.startswith('bypass-'):
            assert audit_exit==1 and len(report['violations'])==1 and report['target_attempts']==1,report
        else:
            assert audit_exit==0,report
            if case.startswith(('blocked-','dns-')) or case in ('mixed','mixed6','additional','reserved-alias','inconsistent','aliases','addresses','records','oversize','total','dname','nat-app','capacity'):
                assert report['target_attempts']==0,report
            else:
                assert report['target_attempts']>0,report
        assert 'secret-sentinel' not in child.stdout,'normal test output leaked URL/body sentinel'
        results.append({k:report[k] for k in ('case','child_exit','audit_exit','dns_attempts','target_attempts','duration')})
        print(json.dumps(results[-1]),flush=True)
    events=[json.loads(x) for x in out.joinpath('events.jsonl').read_text().splitlines()]
    assert any(x['kind']=='http' and x['host']=='ok.example.com' for x in events),'no real success'
    assert any(x['kind']=='sni' and x['host']=='cname-tls.example.com' for x in events),'SNI changed to CNAME'
    assert not any(x['kind']=='proxy' or x.get('credential') for x in events),'proxy/credential leak'
    assert not any(x['kind']=='http' and x['ip'] in ('169.254.169.254','fd00::1','93.184.216.36') for x in events),'firewall payload escaped'
    assert sum(x['kind']=='rebind' for x in events)==1,'rebind fixture did not change'
    assert sum(x['kind']=='http' and x['host']=='rebinding.example.com' for x in events)==1,'rebind sent duplicate HTTP'
    assert not any(x.get('host')=='internal.example.com' for x in events),'redirect target resolved'
    out.joinpath('summary.json').write_text(json.dumps(results,indent=2)+'\n')
    fw4=run('iptables-save','-c');fw6=run('ip6tables-save','-c')
    assert re.search(r'\[[1-9]\d*:\d+\].* -j REJECT',fw4),'IPv4 rejection control did not reach firewall'
    assert re.search(r'\[[1-9]\d*:\d+\].* -j REJECT',fw6),'IPv6 rejection control did not reach firewall'
    assert re.search(r'\[[1-9]\d*:\d+\].* -j DNAT',fw6),'translation control did not exercise DNAT'
    out.joinpath('firewall-v4.txt').write_text(fw4)
    out.joinpath('firewall-v6.txt').write_text(fw6)
    resources={p:Path('/sys/fs/cgroup/'+p).read_text().strip() for p in ('memory.max','cpu.max','pids.max')}
    assert resources['memory.max']=='134217728' and resources['pids.max']=='128',resources
    quota,period=map(int,resources['cpu.max'].split());assert quota<=period,resources
    out.joinpath('resources.json').write_text(json.dumps(resources,indent=2))
finally:
    fixtures.terminate();fixtures.wait(timeout=5)
