"""Deny-only operator approval checks; no resolver, network or credential I/O."""
import datetime,ipaddress,json,os,re
from pathlib import Path
from urllib.parse import urlsplit

def instant(s):return datetime.datetime.fromisoformat(s.replace('Z','+00:00'))
def now():return datetime.datetime.now(datetime.timezone.utc)
def fresh(a):
 start,end=instant(a['approved_at']),instant(a['expires_at'])
 return start<=now()<end and datetime.timedelta(0)<end-start<=datetime.timedelta(hours=24)
def load_approval(path,host,ip):
 p=Path(path);s=p.lstat()
 assert p.is_absolute() and p.is_file() and not p.is_symlink() and s.st_uid==os.getuid() and not s.st_mode&0o022 and s.st_size<=8192
 a=json.loads(p.read_text());u=urlsplit(a['url'])
 assert u.scheme=='https' and u.hostname==host and u.port in (None,443) and not u.username and not u.password and not u.fragment
 assert fresh(a) and re.fullmatch('[0-9a-f]{64}',a['dns_digest'])
 assert 1<=len(a['addresses'])<=32 and ip in a['addresses']
 for x in a['addresses']:
  addr=ipaddress.ip_address(x);assert addr.is_global and not addr.is_multicast and not addr.is_unspecified
 return a

def bound(a,line):
 return fresh(a) and all(a[k]==line[k] for k in ('workspace_id','project_id','monitor_id','configuration_version'))
def permits(a,line,r):
 u=urlsplit(a['url'])
 return bound(a,line) and r['host']==u.hostname and r['scheme']=='https' and r['port']==443 and r['ip'] in a['addresses'] and r.get('dns_digest')==a['dns_digest'] and instant(r['deadline'])<=instant(a['expires_at']) and instant(r['deadline'])>now()
