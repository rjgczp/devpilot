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
