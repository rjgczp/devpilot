# Task6.5 — isolated independent stop and recovery

This is only the approved no-upstream Linux fixture. Ordinary builds still have
no execution entry, and the original input-only Probe seal / HTTP execution
kernel remain unchanged. The streaming adversarial fixture Probe is a lifecycle
witness, not completion of6.3's original-kernel combined execution acceptance.
There is no public stop API, production mode or environment-variable unlock.

## Separate authorities and evidence

The trusted outer Supervisor owns only its newly allocated, labeled containers,
private management pipes and an Ed25519 signing key in a0700 management directory.
No Worker/Probe has its Docker API, key, state directory or management pipes.
Network managers retain exactly NET_ADMIN. A dedicated Linux PID1 lifecycle
Supervisor has only KILL, SETUID, SETGID and SETPCAP; no NET_ADMIN/SYS_ADMIN,
privileged mode, host PID/network, Docker socket, source/config volume or DB
credentials. This additional OS capability set is solely for the independent
fixture lifecycle boundary; no database grant is added.

Before exec, the child loses every capability bounding bit, supplementary group
and root identity. Its five capability sets are0, UID/GID65534, no_new_privs1;
environment is exactly LANG=C/LC_ALL=C/TZ=UTC. Existing6.4 checks enforce readonly
root,128MiB/CPU1/PID128/FD256 and absent credentials/admin mounts. Actual attempts
to read PID1's private fds/memory/environment, signal PID1, change routes/rules,
create raw sockets or enter a namespace must fail. Fixed fixture configuration
and certificate pass over a private anonymous pipe, not argv/environment.

Stop is independent of Worker cancellation, leases, API and database liveness:

1. Bind the actual owned network namespace plus complete scope/run/v/g/epoch/
   fence/Supervisor/namespace incarnations to the exclusive private bootstrap
   ledger. A recovery revoker can act even after the original control process
   exits or policy tables disappear. Mismatched control is rejected.
2. Durably tombstone the incarnation; atomically replace the Gateway rules with
   dual-stack DROP for input/output/forward, without ESTABLISHED exemptions.
   Delete the Run-facing veth, flush IPv4/IPv6 conntrack and independently verify
   absent wire, empty conntrack and terminal DROP. Apply and verify the same
   terminal state in the Run namespace. No expired grant is renewed.
3. While the fixture Probe still holds its original established TLS socket,
   instruct an additional write. The independent receiver must see no new
   frame. For live-permit cases, kernel millisecond expiry samples conservatively
   prove revocation preceded natural expiry, so TTL expiration cannot substitute
   for emergency stop. A second connection is not used as this witness.
4. PID1 enumerates its dedicated PID namespace, retains pidfds plus original
   start ticks, SIGSTOPs then SIGKILLs every other process and reaps descendants,
   including intentionally orphaned grandchildren ignoring TERM/INT. A bounded2s
   scan must reach an empty domain; every captured pidfd must report exit. PID
   reuse cannot redirect a pidfd; an unresolvable race fails closed. Missing,
   oversized or uncertain confirmation means no receipt.
5. Capture the dead child's bounded strace through the trusted PID1 IPC, then
   recheck the empty domain and retire PID1. The kernel PID namespace cannot
   spawn again after its init exits. This occurs before outer fixture cleanup.
   The stopped Run netns may remain pinned by a management process for evidence,
   but its wire is gone, bootstrap consumed and it cannot regain an old permit.
6. Only then sign the bounded immutable receipt. It binds the entire lineage,
   actual PID/net namespace identities, process count and SHA256 of independent
   network, pidfd/reap and retirement proofs. The PG18 test pins the management
   public key on its private control channel before Start and verifies Ed25519
   and exact lineage. A Worker-supplied public key is never a trust anchor.

The receipt contains no URL, target IP/DNS, headers/body or credentials. Raw
fixture network/process witnesses stay in separate hash-indexed test artifacts.
The signing state burns a Run before allocation, stores the exact signed receipt
and records receipt consumption with exclusive files and fsync. Reloading the
same trusted state rejects old Runs and consumed receipts. Duplicate evidence
may be read for reconciliation, never used to execute that Run again.

## Stop / recover / unable to confirm

This fixture has private stdio operations, not an operator-facing production
service. Use the dedicated runner below in a fresh disposable topology. A trusted
operator must stop via the independent Supervisor, retain both network and
process proofs, verify the pinned key/lineage/signature and record the outcome.
Do not use Worker reports, DB fields, process-disappearance guesses or Docker
container removal as termination evidence.

If the lifecycle Supervisor is lost, malformed, times out, cannot reap all PIDs,
loses its trusted ledger/key or cannot verify either network layer: deny new
capabilities, leave all32/4/2/1 and target slots held, preserve quarantine and
raw evidence. Kernel permit expiry limits network access but is not a release
receipt. Independent revocation may remove the wire without proving process
termination; that still cannot release capacity. The explicit fault injection
kills PID1 through Docker and intentionally issues no receipt, even though Linux
may kill its descendants. Later container deletion is cleanup only.

Recovery reloads the same trusted management state/public-key pin, reconciles
immutable receipts and first revokes old physical wires. Never reset a ledger,
change a key or assign a new namespace incarnation to restore the same Run.
Fresh namespaces cannot accept old control/installation messages; old namespace
bootstrap remains consumed. A new Run/epoch can only come from a separately
validated normal scheduling/Owner authorization path. No old GET is replayed.
If old-node isolation is uncertain, keep blocked rather than replacing it with
an apparently available node. Trusted state loss requires independent review;
new keys or an empty directory are not recovery credentials.

**Task5.4 is deliberately still pending:** this batch introduces no DB receipt
submission/release function, EXECUTE, role/column/table grant or Migration change.
Even after valid physical proof, the real PG test confirms reservation_state is
reserved, release timestamps/termination_receipt NULL and original slots held.
A future narrowly authorized5.4 protocol must separately review persistence,
replay/idempotency, parent-lock release and all retained target-key generations.
Task6.3 combined execution is also pending. No result/unknown transition system,
periodic recovery or production deployment is completed here.

## Reproduce and inspect

With native Linux Go1.24.3/Goose3.26.0, Docker, IPv6 and nft/conntrack/pidfd support:

```sh
docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
STOP65_TOOLS=/absolute/fresh/tools sh tests/scheduler/build-stop65.sh
python3 tests/scheduler/run-network64.py --tools /absolute/fresh/tools --evidence /absolute/fresh/network64
python3 tests/scheduler/run-stop65.py --tools /absolute/fresh/tools --evidence /absolute/fresh/stop65
```

The original76+5, Snapshot, full PG18 normal/race and API zero-target audits
remain in their existing CI paths. No skip/retry or simulated network/process
proof is used. Missing tools, capabilities, pidfd, IPv6, confirmations or raw
trace produce failure. The dedicated6.5 PG wrapper uses a20s selected-target
permission (within the unchanged35s hard limit) to establish and stop a live TLS
flow; the historical6.4 case retains its5s request unchanged. This is a new
live-flow test, not an extension of a failed test's timeout. All Go process
budgets, DB lock/transaction limits and original safety assertions remain.

Review execution-security Artifacts: stop65-evidence/{linux-termination,
pg-normal,pg-race}/ contains namespace/rules/routes/iptables/ip6tables/conntrack,
process status/start ticks/pidfd confirmations, receiver stream events, raw
strace, signed receipt/public pin and real PG ledger/lock/SQLSTATE evidence.
Each command receipt binds actual HEAD, dirty status, full source inventory,
command/exit, original log hash and binaries. Development failures remain in the
change evidence index; only frozen-HEAD Push/PR results count as final CI.
