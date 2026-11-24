# Private HTTP loop — isolated validation only

From the repository root, run `./scripts/mvp.sh private`. Requirements: Linux
containers with the existing Docker veth/nft fixture support, Python3, Node/npm,
OpenSSL3 (ED25519), and the pinned Go1.24.3/PG18 images. On macOS select
Homebrew OpenSSL3 in PATH. No external Linux host or production database is used.
This creates a disposable cluster; it never reads the ordinary management DSN.

Open http://127.0.0.1:5174 using a separate browser profile from management mode.
The terminal identifies a0600 temporary Owner credential file in the0700
`.devpilot-private-loop` directory. Log in, select Workspace/Project and authorize
the fixed Monitor. No script supplies that consent for the interactive demo.
The native `privateworker` and `privateprobe` perform at most three actual60s
cycles. Open History to see Run status, HTTP200, duration and the live trend.
The Gate then closes; history remains readable for the bounded15-minute session.
Pause stops future approvals; in-flight work still requires trusted physical
termination. Ctrl-C tears down owned resources; teardown is never release proof.

The fixed target is an HTTPS receiver on independent IPv4/IPv6 veth networks
with **no upstream**, not an Internet service. The API rejects configuration
writes; the keyed catalog/Permit also binds exact URL/version/address. API/PG
and the two cap0 Workers use a separate internal control network. Probe has no
DB secret or Supervisor control pipe. The existing root allocator and signed
PID1/namespace/guard stop proofs remain outside Worker/Probe.

### History and AI demo

The demo API now mounts the existing AI handler over the same fixture database
and member-scoped history reader. After three actual cycles, History and the
trend retain those stored Run IDs and the AI button can analyze them. The
default model is an explicitly labelled **model Mock**; it cites the supplied
Run IDs and measurements and never claims a real-model or Internet result.

If port5174 belongs to another project, use
`DEVPILOT_DEMO_PORT=15174 ./scripts/mvp.sh private` and open
`http://127.0.0.1:15174`. All listeners remain loopback-only. CI keeps5174.

Real DeepSeek is opt-in, after model-call authorization, using
`DEVPILOT_DEMO_AI_CONFIG_FILE=/absolute/private/model.json`. The0600 JSON holds
`{"base_url":"https://api.deepseek.com","model":"deepseek-flash","api_key_file":"/absolute/private/key"}`;
the Key file must also deny group/other access. Keep both beneath
`.devpilot-private-loop` (denied by Vite) or outside the served source. Never put
the Key in the JSON, environment, browser or logs. The host-side demo backend
alone reads it. Only fixed DeepSeek HTTPS requests cross private Docker stdio;
no network, DNS/IP policy or upstream route is added to PG/Worker/Probe. The
transport disables provider thinking for the existing800-token response cap.
Real mode allows at most3 upstream requests for this bounded demo lifetime,
with no automatic retries; each diagnosis retains the existing3-step/20s cap.
`demo-model-usage.json` records only usage/latency and cited Run IDs.

The browser demo test also checks the AI result against actual History IDs.
For an interactive local review, `DEVPILOT_DEMO_KEEP_RUNNING=1` skips the test's
stop-file signal; the original15-minute demo bound and three-cycle stop remain.
The disposable data is never imported into the production management database.

The default `privateworker` entry still stays closed. `--private-isolated`
additionally needs the real approved private Gate, registered restricted Worker
identity, operator catalog, and inherited anonymous Supervisor pipes (read3,
write4). The bounded typed protocol has no SQL transport or reconnect. Lost ACK,
Supervisor, Start or Result stops the process; restart never reconstructs a
started GET. The trusted allocator in this validation is the existing Linux
fixture; an unattended production Supervisor launcher is not delivered here.

`PERMISSIONS.md` and `private-mode-diff.json` describe the exact SQL candidate.
It remains outside Goose migrations. Only this explicitly approved disposable
test applies it after checking its hash against `isolated-test-approval.json`.
Applying it leaves the deployment Gate closed. Production application and any
real Internet egress require separate authorization. Migration11 stays absent;
Migration1–13, roles, table/column ACLs and2s/250ms budgets are unchanged.

Directed validation: `sh tests/privateloop/run.sh /absolute/new/evidence-dir`.
Each scenario owns a fresh disposable cluster: three60s cycles; Owner pause;
real60s expiry; Supervisor loss after an earlier success, unknown isolation,
retained capacity and Worker restart without resend. Evidence includes raw
Run/Result/due and receiver GET counts; transient DSNs/frames are redacted.
Browser test: start the private mode, then
`DEVPILOT_PRIVATE_LOOP=1 npm --prefix web run test:demo`.

Remaining: production candidate approval/application, production identities and
trusted allocator deployment, separately authorized real egress, actual VPN/TLS/
firewall/key-file boundaries. Disk capacity proof remains deferred (Task8.3).
Historical55P03/lock-wait, IPv6 and management-timeout uncertainty is retained;
passing these directed tests does not establish their root causes. OpenSpec37/47.
P2: interrupting a failed browser run can race the inherited fixture cleanup
against the auto-removed control coordinator. Inspect the owned run label for
leftover disposable resources; a cleanup error or deletion never proves release.
