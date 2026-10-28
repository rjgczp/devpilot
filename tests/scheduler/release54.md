# Task5.4 — physical quarantine and database settlement

Fixture-only extension of the independently reviewed6.5 Supervisor. Production
execution remains hard closed. Ordinary release builds and API dependencies do
not gain a runner, public stop endpoint, key registration or network capability.
This is not task6.3 combined original-kernel execution or result-system delivery.

The human separately approved exactly the three signatures and trust boundary
in `docs/monitor-scheduler-task54-capabilities.md`. No other SECURITY DEFINER
function, role attribute, runtime table/column grant or secret permission is
introduced. The private owner's existing scheduler privileges are unchanged.
Migration1–7 remain byte-identical; the unmerged Migration8 changes only affect
fresh guarded disposable tests until a separately authorized future rollout.

## Authorities and order

Trusted offline fixture registration pins the public PEM in the existing
Supervisor control row. `prepare_run` freezes its SHA256 in that Run's reservation.
Worker cannot register or replace a key. Missing registration remains valid for
the historical no-release tests but grants no new release ability. Resetting a
registry/key or using a new namespace/incarnation is not recovery authority.

The independent Supervisor first revokes both network domains, confirms actual
pidfd exits and orphan reaping, and retires PID1. Its private Authority fsyncs
immutable matching proof files before signing and committing the receipt. The
fixture adapter verifies the pinned Ed25519 key, complete ten-field lineage,
proof hashes and physical invariants **before opening a DB transaction**. Raw
proofs are bounded32768bytes; receipts4096bytes. No URL/DNS/IP/header/body or
credential enters the receipt or Run ledger.

PL/pgSQL does not verify Ed25519 or inspect Linux. Only the trusted Supervisor
SQL identity can attest acceptance, matching the explicitly approved boundary;
Worker/API/Controller cannot call accept or reconciliation. Compromise of that
trusted authority is not solved by an SQL flag or this fixture protocol.

Recovery uses Workspace→Project→Monitor→Activation→Schedule→Run→global/W/P/M
budgets→sorted retained origin keys→sorted retained IP keys→Reservation→control.
It does not take a User, Session or Membership lock. After all waits a fresh SPI
command and saved DB clock verify identity/key/full old Run lineage. Pause,
PATCH, Owner loss or authorization/Lease expiry do not relabel old physical
proof into a new grant. The original Run namespace/key/Fence must still match.

`quarantine_physical` only isolates a prepared Run and keeps all counters held.
`accept_termination` atomically consumes the unique nonce/exact receipt digest,
marks reservation/run physical release, returns32/4/2/1 and every retained target
concurrency slot once. No token, rate, not_before, keep_until or retention count
is refunded. It writes no HTTP result, attempt state, next_due or authorization.
A still-started Run may continue to block its Monitor's next Claim under the
existing outstanding index until the future result/unknown protocol closes it;
that index is not weakened here.

## Stop, uncertainty and manual recovery

1. Detect Lease/ACK/Worker/node uncertainty and isolate the exact prepared Run
   using the Controller capability. Reserved or quarantined capacity remains
   occupied. Do not wait for a Lease/TTL to turn into release evidence.
2. Stop through the independent Supervisor. Existing TLS-flow cut, process tree,
   PID namespace retirement and immutable journal must all be proved. Sending a
   resume signal, deleting a container, observing a vanished PID or expiring a
   network rule is insufficient. Missing evidence means no accept call.
3. Deliver a verified receipt once to the originally registered Supervisor's
   bounded transaction. A normal committed response establishes consumption.
   A duplicate delivery rejects; it is not an idempotent release command.
4. If Commit ACK is lost, retain the exact immutable bytes, receipt UUID and
   digest. Use only `reconcile_termination` for that full scope and original
   identity. Consumed means the original transaction committed; pending does
   not trigger an automatic accept retry. Unknown query results remain blocked.
   Neither outcome reissues input, a target permit, the old Run or its GET.
5. A later manual redelivery is possible only as an explicit trusted recovery
   operation after reconciliation confirms no prior consumption and the same
   committed evidence/key/identity is revalidated. It cannot use a newly signed
   replacement receipt to guess what happened. Concurrent consumers are fenced
   by the same parent/budget/reservation locks and unique consumption constraints.
6. Lost Supervisor/state/key, replaced incarnation, inconsistent ledger, missing
   proof or unavailable DB requires preserving quarantine and raw evidence for
   independent review. Never decrement counters, edit released_at, renew the
   old network permit, reset the trusted journal or run Goose Down as recovery.
   Stopped/expired identity must be independently revalidated through trusted
   deployment provisioning before resource accounting can proceed. Physical
   emergency stop itself does not depend on DB liveness.

A full32 quarantined slots can block all new work. This is intended safety-first
behavior; pausing or PATCH remains available. It is not evidence that the target
failed, nor permission to replace a lost node with another execution of its Run.
There is no production/manual SQL repair procedure in this fixture deliverable.

## Reproduce and inspect

```sh
RELEASE54_TOOLS=/absolute/fresh/tools sh tests/scheduler/build-release54.sh
python3 tests/scheduler/run-release54.py --tools /absolute/fresh/tools --evidence /absolute/fresh/release54
```

Native Go1.24.3, Goose3.26.0, guarded PG18, Linux IPv6/network privileges and
pidfd support are mandatory. Every command is recorded; no skip or retry loop.
Each normal/race test owns a fresh network-none PG18, isolated Run/guard/receiver
and private lifecycle authority. SIGSTOP affects only lifecycle PID1 to produce
an actual surviving unprivileged Probe tree past the original45s Lease; network
TTL stays<=35s and cannot be renewed. Resume must be followed by the existing
explicit scoped stop command, with the original3s management confirmation and
2s pidfd stop bound. Recovery never relies on SIGCONT as a termination proof.

Raw artifacts include before/after Lease PID snapshots, both namespace rules,
veth/conntrack removal, pidfd/reap and retirement proofs, pinned public key,
committed bundle and receipt hashes, actual SQLSTATE/pg_locks/blockers/commit
barriers and final counters. DML fault injection and real Commit-before-lost-ACK
must be distinguished from an actual rollback. Development failures remain
recorded even after correction; only exact frozen-SHA actual CI is final proof.
