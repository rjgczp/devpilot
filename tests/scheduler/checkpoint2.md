# Checkpoint2 database protocol

Only OpenSpec tasks4.1–4.6 are implemented here. The scheduler package remains
Linux fixture-only. No controller command, Probe or Supervisor is started.
Production execution is closed. These database fixtures are not physical-stop
or three-cycle network evidence.

## State dimensions

| Dimension | Values / authority |
|---|---|
| Intent | Activation enabled/disabled; Owner API only; independent of attempts |
| Authorization | Schedule v/g/epoch plus DB issued/expiry; no automatic renewal |
| Scheduling | Disabled / authorized waiting / due / blocked projections |
| Attempt | leased → started → completed or unknown; leased → canceled/expired |
| Physical capacity | reserved / quarantined / released, independently of result |

Terminal attempt state/result/finished time and lineage are immutable. Unknown
has null HTTP status, success and failure_code, never a synthetic HTTP failure.
Started→canceled also requires physical release proof. Exhaustive36-pair real
PG transition tests include rejected backwards and shortcut transitions.

Claim locks separate statements in this order: hinted Owner User SHARE,
Workspace NO KEY UPDATE SKIP LOCKED, hinted Membership UPDATE, Project UPDATE,
Monitor UPDATE, Activation UPDATE, Schedule UPDATE SKIP LOCKED, outstanding or
provisional Run, global/W/P/M budgets, Reservation, control. Candidate reads take
no row locks. Failed/skip candidates roll back all locks; Owner selection occurs
only in a new transaction. Every candidate is READ COMMITTED with2s transaction
and statement budgets and250ms lock budget. Scan64W ×8M, tick32 attempts, at most
one successful Claim per W; cursor rotation is explicit. Tick itself performs
no automatic retry or timer loop.

The final gate follows all waits and uses one observed clock_timestamp plus the
maximum persisted schedule/worker/claim time. A backwards step larger than the
existing2s transaction budget fails closed with clock_untrusted; smaller steps
cannot decrease durable times. Authorization comparisons use that gate, never
the outer statement's start time. Forward movement across an expiry rejects
Claim/Start; a gate beyond the2s transaction budget also rejects. Tests model
clock regression with future persisted timestamps; they do not modify the host
clock or attest a physical Supervisor's monotonic deadline.

## Unknown Claim and unstarted expiry

reconcile_claim is read-only, scoped to W/P/M/run and session_user/incarnation.
Only the exact still-leased, current, unexpired lineage can be returned. No URL
or execution capability is returned. Start revalidates independently. Missing,
expired, foreign or started runs do not authorize a retry or replacement Run.
Actual commit then dropped ACK and actual rollback then dropped ACK are tested.

expire_unstarted locks W→P→M→A→S→Run→Budgets→Reservation→control, with no late
User/Session/Membership access. It only terminates an expired, never-Started Run
with the matching fence. Any Worker must first prove its own registered login
and incarnation; it cannot acquire generic table write or Supervisor rights.

A reservation with every preparation/namespace/input/target field null proves
that physical execution authority never existed. Only the private function
owner may mark release_basis=never_prepared in that exact expired-leased path;
termination_receipt remains null. This is not a forged Supervisor receipt.
Concurrency slots are then returned exactly once; consumed rate tokens and
terminal retention capacity are not refunded. The next cycle gets a new UUID
and due=max(DB gate+interval,last_started+interval,not_before). An expired
permission does not gain a runnable cycle; changed lineage is not overwritten.

A prepared reservation remains quarantined and retains capacity even if no
Start was recorded. A started Run is rejected by this recovery function. Both
require later independent physical termination; this checkpoint provides no
such Supervisor implementation or proof. A45s real DB lease test covers all
three paths, repeated expiry rejection, stale fences, absent active Owner and
no automatic retry. The test's administrative preparation is explicitly a
projection fixture, never a Linux namespace or trusted physical receipt.

Only the two newly authorized narrow EXECUTE grants are added. Runtime role
attributes, schema access, table/column ACLs and secret-reading privileges are
unchanged. Migration1–7 are unchanged. Migration8 remains an un-deployed change.

## Evidence interpretation

Barrier receipts contain pg_locks, pg_blocking_pids and DB timestamps. Commit
acknowledgement observations are upper bounds, not exact server commit times;
lease gate times and preceding barrier observations bound the transaction.
NOWAIT probes and transaction-lock disappearance supplement relation locks,
which alone cannot prove which row is held. SQLSTATEs distinguish authority
refusal, skip, overflow, injected lock timeout, cancellation and deadlock.

The external deadlock test deliberately gives a guarded administrator fixture
an invalid budget→W order and lower deadlock detection delay. Runtime functions
cannot take that path. The resulting40P01 tests rollback handling; it is not
claimed as a deadlock of the approved Worker protocol. Normal protocol tests
assert causal locks/outcomes rather than inferring safety from no deadlocks.
