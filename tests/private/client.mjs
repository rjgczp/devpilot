// Real TLS/API test client inside an internal network, with no DB credentials.
import https from 'node:https';
import fs from 'node:fs';
import assert from 'node:assert/strict';

const input = JSON.parse(fs.readFileSync(0, 'utf8'));
const origin = `https://private.devpilot.test:${input.port}`;
const ca = fs.readFileSync('/ca.crt');
let cookie;
function req(path, method = 'GET', data, headers = {}) {
  return new Promise((resolve, reject) => {
    const body = data === undefined ? undefined : JSON.stringify(data);
    const r = https.request({hostname: 'private.devpilot.test', port: 8443, path, method, ca,
      lookup: (_host, _options, done) => done(null, [{address: input.ip, family: 4}]),
      headers: {Host: `private.devpilot.test:${input.port}`, Origin: origin, 'X-DevPilot-CSRF': '1',
        'Content-Type': 'application/json', ...(cookie ? {Cookie: cookie} : {}), ...headers}}, response => {
      let text = '';
      response.on('data', b => text += b);
      response.on('end', () => {
        let json; try { json = JSON.parse(text); } catch { /* static page or gateway error */ }
        resolve({status: response.statusCode, headers: response.headers, json, text});
      });
    });
    r.setTimeout(15000, () => r.destroy(new Error('TLS request deadline')));
    r.on('error', reject);
    r.end(body);
  });
}

if (input.mode === 'denied') {
  for (const path of ['/', '/api/v1/auth/me', '/api/v1/auth/login']) {
    const r = await req(path, 'GET', undefined, {'X-Forwarded-For': '127.0.0.1', 'X-Real-IP': '127.0.0.1', Forwarded: 'for=127.0.0.1'});
    assert.equal(r.status, 403, 'noninvited network must remain denied despite forwarded headers');
  }
  console.log('PASS: denied network and forged forwarded headers');
} else if (input.mode === 'rate') {
  const statuses = await Promise.all(Array.from({length: 24}, (_, i) => req('/api/v1/auth/'+(i % 2 ? 'login' : 'register'), 'POST', {}, {'X-Forwarded-For': '10.1.1.'+i})));
  assert(statuses.some(r => r.status === 429), 'shared login/registration peer limit must reject bursts');
  assert(statuses.filter(r => r.status === 429).every(r => r.headers['retry-after'] === '60'));
  assert.equal((await req('/api/v1/auth/me')).status, 401, 'rate rejection must not block session reads');
  console.log('PASS: login/registration shared rate limit, Retry-After, no XFF bypass');
} else {
  const index = await req('/');
  assert.equal(index.status, 200, 'real compiled Dashboard must load');
  const script = index.text.match(/src="(\/assets\/[^\"]+\.js)"/);
  assert(script, 'compiled React entry must be present');
  assert.equal((await req(script[1])).status, 200, 'compiled JS must load through the same private boundary');
  assert.equal((await req('/api/v1/auth/register', 'POST', {}, {Origin: 'https://evil.example'})).status, 403);
  assert.equal((await req('/api/v1/auth/register', 'POST', {}, {'X-DevPilot-CSRF': ''})).status, 403);
  assert.equal((await req('/api/v1/auth/login', 'POST', {}, {Origin: 'https://evil.example'})).status, 403);
  const registration = await req('/api/v1/auth/register', 'POST', {email: input.email, password: input.password});
  assert.equal(registration.status, 201, 'register with restricted database identity');
  const login = await req('/api/v1/auth/login', 'POST', {email: input.email, password: input.password});
  assert.equal(login.status, 200, 'login with restricted database identity');
  const set = login.headers['set-cookie']?.[0];
  assert(set?.startsWith('__Host-devpilot_session='), 'production cookie prefix');
  assert(set.includes('Secure') && set.includes('HttpOnly') && set.includes('SameSite=Lax') && set.includes('Path=/') && !set.includes('Domain='));
  cookie = set.split(';')[0];
  assert.equal((await req('/api/v1/auth/me')).status, 200);
  let r = await req('/api/v1/workspaces', 'POST', {name: 'Private test workspace'});
  assert.equal(r.status, 201, 'workspace creation');
  const wid = r.json.data.id;
  r = await req(`/api/v1/workspaces/${wid}/projects`, 'POST', {name: 'Private test project', description: ''});
  assert.equal(r.status, 201, 'project creation');
  const pid = r.json.data.id;
  const collection = `/api/v1/workspaces/${wid}/projects/${pid}/monitors`;
  r = await req(collection, 'POST', {name: 'Execution stays closed', url: 'https://probe-a.example.com/not-executed', method: 'GET', interval_seconds: 60, timeout_seconds: 5, expected_status: 200});
  assert.equal(r.status, 201, 'monitor creation with unchanged invoker triggers');
  const monitor = collection+'/'+r.json.data.id;
  r = await req(monitor, 'PATCH', {name: 'Edited private monitor'}, {'If-Match': r.headers.etag});
  assert.equal(r.status, 200, 'monitor PATCH with restricted columns');
  const status = await req(monitor+'/execution-status');
  assert.equal(status.status, 200, 'restricted execution status function');
  const history = await req(monitor+'/history');
  assert.equal(history.status, 200, 'restricted history function');
  assert.deepEqual(history.json.data, [], 'closed environment has empty real history');
  const activation = await req(monitor+'/activation');
  assert.equal(activation.status, 200);
  r = await req(monitor+'/execution-authorization', 'PUT', {configuration_version: activation.json.data.configuration_version,
    consent_profile: 'periodic-http-get-v1', authorization_seconds: 900}, {'If-Match': activation.headers.etag});
  assert.equal(r.status, 200, 'real Owner consent is recorded without enabling execution');
  assert.equal(r.json.data.effective_authorization, false, 'closed gate must remain ineffective');
  assert.equal(r.json.data.blocked_reason, 'scheduler_unavailable');
  const current = await req(monitor+'/activation');
  r = await req(monitor+'/activation', 'PUT', {configuration_version: current.json.data.configuration_version, desired_state: 'disabled'}, {'If-Match': current.headers.etag});
  assert.equal(r.status, 200, 'pause remains usable');
  assert.equal((await req('/api/v1/auth/logout', 'POST', {})).status, 204);
  assert.equal((await req('/api/v1/auth/me')).status, 401);
  console.log('PASS: TLS Dashboard, production Cookie/CSRF, login/register, W/P/M, PATCH, history, consent closed, pause, logout');
}
