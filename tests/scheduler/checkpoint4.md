# Tasks6.1–6.2: confirmed Start and input-only process boundary

This batch has no DNS, target socket, TLS, HTTP, result system or physical release.
5.4,6.3–6.5 and groups7/8 remain incomplete. It never marks database preparation,
input consumption or a successfully exiting process as Supervisor termination
proof. Production execution remains closed; no environment flag opens execution.

## Authorization provenance

The current product-owner supplement explicitly approves the two Checkpoint3
EXECUTEs after independent review of e56c69a5832e88dfaac469071f9ada51a758dadc.
See docs/monitor-scheduler-checkpoint3-capabilities.md. Checkpoint2 grants are a
separate historical authorization. This batch makes **no SQL/ACL change**, including
no new function EXECUTE, role attribute, table/column or secret-read permission.

## Start and single input issuance

Start still uses the already-reviewed restricted VOLATILE function, one candidate
READ COMMITTED transaction/2s and250ms lock timeout. The sole parent direction is
Owner User SHARE → W → Membership → P → M → Activation → Schedule → Run →
global/W/P/M budgets → Reservation → deployment/worker/Supervisor protection.
A fresh SPI statement/DB clock after all waits gates active Owner/membership id,
full scope, current configuration v/g/epoch, consent/expiry, owned lease/incarnation,
fence, remaining timeout+5, hard permit deadline and stop/deployment/HTTP gates.
No joined locking query chooses lock order. No new Owner can be chosen under W.

Store.Start returns its private snapshot only after confirmed Commit and pool
release. Offline configuration validation also uses the original Monitor policy.
Unknown/rolled-back Start ACK returns an empty snapshot; there is no readback
constructor or recovery operation for it. The caller must keep an uncertain
operation blocked rather than retry it. Database transition to unknown/recovery
is group7, not implemented here. A confirmed started Run cannot Start again.

StartSnapshot.IssueInput burns its shared process-local capability at entry,
including failures. It calls existing consume_input using a **separate registered
Supervisor DB identity**, commits that independent once-only record, and releases
the connection. A lost ACK returns no descriptors; the sender cannot reconnect.
Only after confirmed input consumption does it create/fill/close two anonymous
pipes. They are bounded atomic writes (4096 and1024 bytes); no URL goes through
arguments, environment, normal output or Run persistence. Context/deadline/write
failure closes descriptors and does not restore input. There is no background
sender or DB connection during reception. The two pipes are closed at writer end
before being handed to the child, so parsing requires a real EOF.

## Fixed protocol and trusted receiver binding

The4096-byte frame uses an8-byte magic/version,4-byte big-endian payload length,
canonical strict JSON and zero padding. URL uses bounded base64 bytes only inside
this private frame. It binds W/P/M/run, v/g/epoch/fence, confirmed Start time, frozen
configuration, Supervisor/namespace identities and the original immutable hard
deadline. A1024-byte receipt on the second anonymous pipe pins full lineage,
deadline and SHA256 of the entire frame. The receipt is a **trusted descriptor
binding**, not an HMAC key, public bearer token, authentication by an unkeyed hash,
or physical network permission. It is never obtained from a user request.

The receiver requires actual anonymous FIFO descriptors (not a pathname, file,
socket or named FIFO), finite read time, exact size and EOF, canonical JSON
(no duplicate/unknown keys/invalid Unicode), matching digest/full lineage and
unexpired valid timing. Success or failure consumes the receiver once. A new
process using the old readers sees EOF; different-run/scope/version/epoch/fence/
namespace receipts fail. Neither API nor Probe can reconstruct a new confirmed
Start snapshot. The trusted launcher must not mint new pipes from saved bytes;
this protocol does not claim protection against a malicious trusted controller
that fabricates both descriptors. No snapshot or receipt is persisted for replay.

The private monitoring bridge constructs the original unexported Snapshot and
calls its original offline validation. It does not expose an engine constructor
or execution method. Integration with the original private DNS/IP/TLS/HTTP engine
requires the later reviewed6.3 permit hook and independent network authorization;
no weaker client or substitute resolver is added.

## Actual Linux process boundary

The input-only probe command is Linux + schedulerfixture only. Its main never
calls an engine, resolver, dialer or HTTP client. Ordinary builds exclude it;
the API release dependency/symbol checks remain in the historical regression.

The explicit process test creates a network-none, read-only Linux container with
only the two read-only binary mounts, memory128MiB (swap also128MiB), CPU1,
PID128 and FD256. No source, API config, credential volume or Docker socket is
mounted. The trusted test parent retains only SETUID/SETGID/SETPCAP to run setpriv;
the actual child has UID/GID65534, no supplementary groups, **all effective,
permitted, inherited, ambient and bounding caps0**, and no_new_privs. Its explicit
environment has only LANG=C and TZ=UTC despite credential sentinels in its parent.
The cgroup budget constrains the whole fixture process group (stricter than the
Probe alone); dedicated production/run cgroup supervision is not claimed here.

The child installs a TSYNC seccomp filter on every thread, denying all socket
creation/network/management syscalls, exec and namespace management; only runtime
thread clone is retained. This is input-process permission removal, not6.4 network
rules or a grant of network authority. It cannot access PG or a management socket
even through an application bypass. The child verifies actual cgroup/rlimit/root
mount state, all thread cap/NNP/filter state and inherited descriptors. It observes
real EMFILE at FD256. The parent independently reads live /proc status/limits/
cgroup/cmdline before permitting exit. Descriptor/environment/cmdline/diagnostics
are checked for URL/DB/Cookie/cloud sentinels. The command only emits bounded
boolean/numeric evidence. No input or URL is written to ordinary output.

## Reproduction and evidence limits

Build integration,schedulerfixture,budgetfixture binaries and use the existing
checkpoint3-pg.py with explicit TestBudget Start/Input/ClaimPause selections. Each
new top-level test owns a guarded disposable PG18 instance, retains unreleased
rows, asserts unsafe Down refusal and destroys only its owned test server.
Existing history still uses its reviewed fixtures; their cleanup is not physical
proof. Build schedulerfixture,processfixture driver and schedulerfixture Probe,
then run checkpoint4-process.py. Missing seccomp/cgroups/tools/limits fail; no skip.
Normal and race commands, exact source/binary SHA256, SQLSTATE, pg_locks/blockers,
barriers, final held ledger, /proc/cgroup and container inspections are recorded.

Real30s Lease-horizon and real60s authorization/45s Lease historical tests are
separate finite processes; no runtime timeout or durable Lease is shortened.
Tests prove specified interleavings, not absence of every concurrency defect.
PG issuance and standalone sandbox are independent real proofs using the same
protocol/source; this is not the later full Supervisor/periodic runner integration.
No target network76-case or strace target audit is rerun in this no-target batch;
their implementation/whitelists are preserved, and non-network kernel/unit/race
plus historical API/PG regressions are rerun. Full network acceptance awaits6.3+.
