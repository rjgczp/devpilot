"""Demo-only model connection over private Docker stdio; no control/data route.

Default: explicitly labelled model Mock. Real DeepSeek is opt-in via a private
JSON file {base_url, model, api_key_file}. Only this host-side service reads Key.
"""
import base64, http.client, json, os, select, ssl, subprocess, threading, time
from pathlib import Path

BRIDGE = r'''
import base64,json,os,sys
from http.server import BaseHTTPRequestHandler,HTTPServer
class Handler(BaseHTTPRequestHandler):
 def log_message(self,*args):pass
 def do_POST(self):
  self.connection.settimeout(22)
  n=int(self.headers.get('Content-Length','0'))
  if self.path!='/v1/chat/completions' or not 0<n<=24576:
   self.send_error(400);return
  raw=self.rfile.read(n)
  print(json.dumps({'payload':base64.b64encode(raw).decode()}),flush=True)
  reply=json.loads(sys.stdin.readline());body=base64.b64decode(reply['body'])
  self.send_response(reply['status']);self.send_header('Content-Type','application/json');self.send_header('Content-Length',str(len(body)));self.end_headers();self.wfile.write(body)
server=HTTPServer(('127.0.0.1',18082),Handler)
print(json.dumps({'ready':True,'pid':os.getpid()}),flush=True)
server.serve_forever()
'''

def settings():
    path = os.environ.get('DEVPILOT_DEMO_AI_CONFIG_FILE')
    if not path:
        return {'model': 'fixture-model-mock', 'mock': True}
    p = Path(path)
    assert p.is_file() and p.stat().st_mode & 0o077 == 0, 'private demo model config required'
    cfg = json.loads(p.read_text())
    assert cfg['base_url'] == 'https://api.deepseek.com' and cfg['model'] == 'deepseek-flash'
    key = Path(cfg['api_key_file'])
    assert key.is_file() and key.stat().st_mode & 0o077 == 0, 'private server key file required'
    cfg['mock'] = False
    return cfg

class ModelBridge:
    def __init__(self, container, dest, cfg):
        self.container, self.dest, self.cfg = container, dest, cfg
        self.entries = []
        self.key = None if cfg['mock'] else Path(cfg['api_key_file']).read_text().strip()
        assert self.key is None or (0 < len(self.key) <= 8192 and '\r' not in self.key and '\n' not in self.key), 'invalid private model key'
        self.proc = subprocess.Popen(['docker','exec','-i',container,'python3','-u','-c',BRIDGE],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
        assert select.select([self.proc.stdout],[],[],15)[0], 'demo model bridge startup timeout'
        ready = json.loads(self.proc.stdout.readline());assert ready['ready'] and ready['pid'] > 1
        self.pid = ready['pid']
        threading.Thread(target=self.serve,daemon=True).start()
    def mock(self, body):
        tools = [m for m in body['messages'] if m['role']=='tool']
        if not tools:
            message = {'role':'assistant','tool_calls':[{'id':name,'type':'function','function':{'name':name,'arguments':'{}'}} for name in ['get_monitor','get_recent_results']]}
            finish = 'tool_calls'
        else:
            history = json.loads(next(m['content'] for m in tools if m['tool_call_id']=='get_recent_results'))
            rows = history['data']
            observation = '已存储的隔离检查均成功，但不能据此证明公网可用。' if rows and all(r['status']=='success' for r in rows) else '现有证据不足或含非成功结果；Mock 不推断故障根因。'
            result = {'summary':f'Fixture 模型 Mock：读取到 {len(rows)} 条实际隔离检查记录；这不是公网监控或真实模型结论。','evidence':[f"Run {r['id']}：{r['status']}，HTTP {r['http_status']}，{r['duration_ms']} ms" for r in rows[:6]], 'possible_causes':[observation],'confidence':'low','limitations':['模型为明确标记的 Mock；数据由真实隔离 HTTP 周期产生，不代表公网。'],'recommendations':['核对上述 Run 与 History、趋势图的一致性；公网验收等待独立出口和受控目标。']}
            message = {'role':'assistant','content':json.dumps(result,ensure_ascii=False)};finish = 'stop'
        return 200, json.dumps({'choices':[{'message':message,'finish_reason':finish}]},ensure_ascii=False).encode()
    def serve(self):
        for line in self.proc.stdout:
            entry = {'attempt':len(self.entries)+1,'mock':self.cfg['mock']};began=time.monotonic()
            conn=None;status=502;raw=b'{}'
            try:
                assert len(self.entries)<(90 if self.cfg['mock'] else 3), 'demo model budget exhausted'
                self.entries.append(entry)
                body=json.loads(base64.b64decode(json.loads(line)['payload']))
                assert body['model']==self.cfg['model'] and body['max_tokens']==800
                assert all(x['function']['name'] in ('get_monitor','get_recent_results') for x in body['tools'])
                entry['tool_result_ids']=[r['id'] for m in body['messages'] if m['role']=='tool' and m['tool_call_id']!='' for r in json.loads(m['content']).get('data',[])]
                if self.cfg['mock']:
                    status,raw=self.mock(body)
                else:
                    body['thinking']={'type':'disabled'}
                    payload=json.dumps(body).encode();assert len(payload)<=24576
                    conn=http.client.HTTPSConnection('api.deepseek.com',443,timeout=18,context=ssl.create_default_context())
                    conn.request('POST','/chat/completions',payload,{'Content-Type':'application/json','Authorization':'Bearer '+self.key})
                    response=conn.getresponse();status=response.status;raw=response.read(32769)
                    assert len(raw)<=32768 and self.key.encode() not in raw
                data=json.loads(raw);entry['usage']={k:v for k,v in data.get('usage',{}).items() if isinstance(v,int)}
                entry['tools']=[t['function']['name'] for c in data.get('choices',[]) for t in c['message'].get('tool_calls',[])]
            except Exception:
                entry['failed']=True;status=502;raw=b'{}'
            finally:
                if conn:conn.close()
            entry.update(status=status,elapsed_ms=round((time.monotonic()-began)*1000))
            (self.dest/'demo-model-usage.json').write_text(json.dumps(self.entries,ensure_ascii=False,indent=2))
            try:self.proc.stdin.write(json.dumps({'status':status,'body':base64.b64encode(raw).decode()}).encode()+b'\n');self.proc.stdin.flush()
            except (OSError,ValueError):break
    def close(self):
        subprocess.run(['docker','exec',self.container,'python3','-c',f'import os,signal;os.kill({self.pid},signal.SIGTERM)'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=5)
        if self.proc.poll() is None:self.proc.terminate()
        try:self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:self.proc.kill();self.proc.wait(timeout=5)
