# Task5.4 database capabilities — separately approved

Review baseline: `3489a41aa588bff5171347c91a36c6915a81fe45`, branch
`codex/add-monitor-scheduler-worker`, PR11 Open. The current human request
authorizes only task5.4 implementation, and explicitly requires separate approval
before adding SECURITY DEFINER functions or precise EXECUTE permissions. This
document records the separately approved scope below; it is not an acceptance record.

Source of truth: approved Design D2, D4, D6, D8–D9 and D12, tasks5.4 and the
`monitor-worker-security` physical-release requirement. Task6.5 was independently
accepted at this baseline. Task6.3 remains incomplete; this proposal does not
implement the result system, periodic recovery, production execution or a public
stop endpoint. At the pre-approval request point tasks were27/47 with5.4 unchecked. Current implementation/acceptance status is recorded in verification.md.

## Exact proposed functions and grantees

```sql
scheduler_private.quarantine_physical(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_fence bigint
) RETURNS jsonb

scheduler_private.accept_termination(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_receipt bytea
) RETURNS jsonb

scheduler_private.reconcile_termination(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_receipt_id uuid, p_receipt_digest bytea
) RETURNS jsonb
```

| Function | Sole runtime grantee | Bounded capability |
| --- | --- | --- |
| quarantine_physical | devpilot_scheduler_controller | For an existing, prepared Run with matching full scope and Fence, move its reserved physical ledger to quarantined. Already-quarantined is unchanged. Never release capacity, create execution authority, extend Lease/permit, change attempt/result or reopen a released reservation. Conservative isolation can occur before Lease expiry after an uncertain ACK; it is not proof of termination. |
| accept_termination | devpilot_scheduler_supervisor | Consume one independently verified, committed Supervisor receipt for its originally prepared Run/namespace, and atomically mark physical release and decrement exactly the held concurrency slots. No Worker/Controller/API caller. No token refund, result write, new Run, next_due, authorization, network permission or deployment gate change. |
| reconcile_termination | devpilot_scheduler_supervisor | Read the outcome of an uncertain accept Commit for exactly this Supervisor login/incarnation, full scope, receipt ID and exact receipt digest. Return only pending/consumed and the physical release time. A conflicting receipt or identity is rejected; this read cannot consume, release, sign, launch or reissue anything. |

All three are proposed VOLATILE PL/pgSQL SECURITY DEFINER functions owned by the
existing non-LOGIN `devpilot_scheduler_owner`, with fixed
`search_path=pg_catalog,scheduler_private`, fully qualified objects and no dynamic
SQL. Existing bounded READ COMMITTED checks remain: at most2s transaction and
statement budgets, lock_timeout250ms, one modifying operation per transaction.
The reconciliation operation performs no DML. No additional SECURITY DEFINER
helper, overload or runtime EXECUTE is requested.

Exact runtime ACL additions, subject to approval:

```sql
REVOKE ALL ON FUNCTION scheduler_private.quarantine_physical(uuid,uuid,uuid,uuid,uuid,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.quarantine_physical(uuid,uuid,uuid,uuid,uuid,bigint) TO devpilot_scheduler_controller;

REVOKE ALL ON FUNCTION scheduler_private.accept_termination(uuid,uuid,uuid,uuid,uuid,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.accept_termination(uuid,uuid,uuid,uuid,uuid,bytea) TO devpilot_scheduler_supervisor;

REVOKE ALL ON FUNCTION scheduler_private.reconcile_termination(uuid,uuid,uuid,uuid,uuid,uuid,bytea) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.reconcile_termination(uuid,uuid,uuid,uuid,uuid,uuid,bytea) TO devpilot_scheduler_supervisor;
```

Worker, API, legacy API, ordinary users, Controller (for the two Supervisor
functions), and Supervisor (for the Controller function) receive no corresponding
EXECUTE. Role attributes, memberships, existing function grants, table/column
ACLs, CREATE/TEMP and secret access do not expand. Existing private-owner table
privileges already cover the relevant scheduler tables and added columns; no
new table/column grant is requested. Migration1–7 stay byte-identical. Any schema
work stays within the unmerged Migration8 and fresh guarded disposable PG18;
there is no production migration or automatic upgrade of an existing database.

## Signature verification and the actual trust boundary

The trusted, fixture-only Supervisor adapter verifies Ed25519 using its
independently pinned management public key **before opening a business database
transaction**. It verifies the strict bounded receipt and the matching committed
network/process/retirement proof bundle, including all ten lineage fields,
actual network/PID namespaces, both independent network revocations, every
pidfd exit, orphan reaping, empty process domain and PID1 retirement. A valid
signature alone without the matching committed proof bundle is insufficient.

Task6.5's existing private Authority journal will durably commit the immutable
proof bundle before signing its corresponding receipt. The proof SHA256 values
must match the signed payload; private journal registration and consumption are
not replaced by mutable Worker-provided files. Missing, truncated, inconsistent,
uncommitted or uncertain proof gives no database accept call. Raw network and
process evidence stays in restricted fixture evidence, not in Run records.

PostgreSQL's PL/pgSQL function is **not an Ed25519 verifier**. As with existing
prepare_run/consume_input, the independently trusted Supervisor SQL identity
attests that its physical verification succeeded. The SQL function revalidates
the attestation's registered identity, pinned key ID, full lineage, nonce and
single-consumption ledger; it cannot independently inspect Linux or recompute
Ed25519 verification. The narrow EXECUTE therefore grants that trusted
Supervisor a physical-release attestation capability. It is not a capability
for an arbitrary caller passing `verified=true`, a Worker-supplied key or a
lease-expiry assertion. No new crypto extension, external database network call,
custom Ed25519 implementation or secret-bearing database function is proposed.

This distinction is explicit for independent permission review: forged receipts
must be rejected by the trusted signature/proof adapter before SQL, while
direct calls by all untrusted roles must fail at PostgreSQL ACL/identity checks.
The design does not claim protection from compromise of the trusted Supervisor
SQL identity itself.

Trusted offline fixture registration will pin the Supervisor public key/hash in
its existing control row before preparation. Preparation will freeze that key
ID in the reservation, without changing its signature or expanding its
capability. Missing key registration never gains a default key or release
permission. The public key is not a secret, and no runtime key-registration
EXECUTE is requested. Changing current registration cannot replace the original
Run's frozen key/incarnation. Existing6.4/6.5 no-release tests may retain an
unconfigured key; such reservations cannot use the new release function.

The database will retain only bounded release metadata: original pinned key ID,
receipt ID, digest of the exact submitted receipt, signed proof digests and the
physical release timestamp. Nonce/digest uniqueness and immutable released rows
prevent repeat consumption and cross-Run replay. The bytea receipt is bounded
to4096bytes, parsed against the exact schema and not persisted as raw data.
It contains no URL, DNS/IP, headers/body, credentials or HTTP result.

## Transaction protocol and unchanged execution semantics

Quarantine and accept use the approved recovery suborder:

```text
Workspace NO KEY UPDATE
→ Project UPDATE → Monitor UPDATE → Activation UPDATE → Schedule UPDATE
→ Run UPDATE
→ global/workspace/project/monitor budget rows
→ all retained origin keys, then all retained IP keys (sorted full keys)
→ Reservation UPDATE → final control/identity protection
→ fresh SQL/SPI identity and lineage recheck + one saved clock_timestamp()
→ atomic DML → confirmed Commit
```

No current Owner/User/Session/Membership is required for recovery, as specified
by D6. No reverse acquisition or late Owner lookup is allowed. Target keys come
only from this reservation's immutable retained-generation list; after the
Reservation lock the list is revalidated against the locked Run/record. The
global budget lock serializes catalog rotation. No capacity query uses unlocked
COUNT. Any missing/underflowing counter, mismatched key, signature identity,
Fence, namespace or receipt causes full rollback.

Accept atomically stores receipt consumption, changes reservation to released,
sets run.physical_release_confirmed_at, decrements global/W/P/M in_use and the
global physical active_count, and decrements every held retained origin/IP slot
exactly once. Retention counts, consumed tokens, last_refill_at, not_before and
keep_until remain intact. It preserves Run v/g/epoch/fence, attempt state and all
HTTP/result columns. Current Schedule v/g/epoch may have changed after Pause or
PATCH: recovery checks the old Run's immutable lineage, not a newly fabricated
current execution grant. Original preparation identity/key must still match.

This batch does not turn started into unknown/completed/canceled, rewrite a
terminal result or schedule the next cycle. Until the later result/unknown
protocol closes a started attempt, the existing outstanding-Run index may
conservatively continue to block a new Run for that Monitor even after physical
capacity is returned. That index will not be weakened to make5.4 pass.

Physical kill/proof/journal I/O precedes the database transaction; no connection
or business lock is held across network or process operations. Duplicate accept
calls reject, including the identical receipt. Unknown Commit is handled only
by the separate exact-receipt reconciliation query; no automatic accept retry,
Probe launch, replacement permit or GET. Unknown reconciliation remains blocked.
Expired/stopped/unregistered Supervisor identity cannot release via an identity
bypass; trusted revalidation must precede any later attempt. No key reset or
new incarnation can substitute for the originally prepared authority.

## Required real acceptance after separate permission approval

1. PG18 LOGIN ACL matrix before/after, PUBLIC/Worker/API/Controller forgery
   refusal, session_user/incarnation binding and unchanged role/table/column/
   secret permissions; Migration1–7 hashes and real fresh-schema constraints.
2. Live Linux Probe with ignored TERM/INT and orphan descendants: original45s
   Lease expires while the run remains alive in a closed network domain;
   quarantine keeps every physical counter held. Natural35s network expiry is
   not a process termination receipt. The dedicated fixture witness must remain
   credential-free and incapable of restoring network permission. No admin
   UPDATE may shorten Lease or fabricate termination. Exercise an actually
   suspended lifecycle Supervisor as a fault, not an extended network permit or
   simulated clock; independently record the still-live descendants and expired
   kernel allowance. Resume only to enforce the already-expired stop, collect
   actual pidfd/reap/retirement proof, and never restore the old network grant.
3. Independent actual dual-stack network revoke, pidfd kill/reap and namespace
   retirement; committed signed proof then one real PG18 accept. Exact counters
   fall once; tokens/cooldowns/retention/result/activation/configuration do not
   change. Original6.5 hard-deadline behavior and all existing cases stay intact.
4. Two independent consumers and a claimant compete for the final slot under
   real PG barriers: one accept succeeds, the duplicate rejects, capacity is
   neither over-released nor prematurely admitted. Capture pg_locks,
   pg_blocking_pids, SQLSTATE and commit timelines, including Pause/PATCH and
   Owner loss with recovery's no-User suborder.
5. Real failure/rollback before and after each release DML, Commit-then-lost-ACK
   versus actual rollback, unavailable DB/Supervisor, old node reconnection and
   foreign/unknown key/lineage/Fence/incarnation/replayed receipt. Reconcile
   only; no same-Run input or network reissue.
6. Frozen-SHA normal/race plus the existing147 PG18 and dedicated budget/Start
   tests, original76+5,6.4 dual-stack bypass,6.5 physical stop, API zero-target,
   unit/race/vet, Change strict and unchanged historical full-library warnings.
   Raw logs, commands/exits, receiver/kernel/process proofs and SHA/hash bindings
   are required before checking5.4; missing real evidence cannot be simulated.

## Approval scope and implementation limit

Requested approval is exactly the three SECURITY DEFINER signatures and three
runtime EXECUTE additions above, with the stated trusted-Supervisor attestation
boundary. At the original pre-approval stopping point no SQL function/GRANT, schema
change, release adapter or executable behavior had been installed, and no5.4
acceptance was reported. The explicit approval below subsequently permitted
implementation within5.4; if a fourth
function/extra grant or different trust boundary is needed, it requires another
request. Production execution stays hard closed. PR11 remains Open; no merge,
Archive, deployment or task6.3 advancement.

## Explicit approval received

The human reply to this exact request was: “批准仅上述3项能力及信任边界”.
This approves only the three signatures and grants above, including the
Supervisor-side Ed25519/proof verification and database attestation boundary.
It does not approve additional functions, result-system work or production use.
