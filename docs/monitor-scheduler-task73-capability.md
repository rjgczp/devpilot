# Task7.3 narrow recovery capability — separately approved

Date:2026-10-27. Baseline local/origin/PR11 head:
`e767a5ad961a47cd323768aaafdd99ca83b55bbe`; clean before this document.
Main/origin main:`1d3b8266e494061beccc3ccdf47694ba4b0339c5`.
OpenSpec31/47. The user explicitly authorized entry into7.3, not7.4 or later.
This does not assert an independent7.2 review signature. Previous technical
acceptance, retained risks and scope restrictions remain in force.

## Why existing grants are insufficient

- `expire_unstarted(uuid,uuid,uuid,uuid,uuid,bigint)` is Worker-only and refuses
  started Runs. Expanding it to terminalize started Runs would exceed its
  independently approved capability.
- Controller `quarantine_physical(uuid,uuid,uuid,uuid,uuid,bigint)` changes only
  physical isolation, expressly not attempt state or scheduling.
- Supervisor `accept_termination`/`reconcile_termination` only attest/reconcile
  physical release. They must not be repurposed as HTTP result/recovery writers.
- Worker `commit_result` only writes confirmed observations; `reconcile_result`
  is read-only and cannot mark unknown/canceled or advance a future identity.
- Worker `candidates` intentionally excludes outstanding started/quarantined Runs;
  the existing recovery path receives the known complete Run identity. No global
  Run enumeration, arbitrary table read or additional discovery grant is proposed.

There is no least-privilege existing path to perform Task7.3 transitions. This
proposal requests exactly one new EXECUTE and no expansion of existing functions.

## Approved signature and sole grantee

```sql
scheduler_private.recover_started(
 p_incarnation uuid,                  -- authenticated Controller incarnation
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_v bigint, p_g bigint, p_epoch bigint, p_fence bigint,
 p_namespace uuid, p_supervisor uuid,
 p_reason text
) RETURNS jsonb
```

Twelve arguments:5UUID,4bigint,2UUID,1text. Only existing
`devpilot_scheduler_controller` receives EXECUTE. Existing non-LOGIN
`devpilot_scheduler_owner` owns a restricted VOLATILE SECURITY DEFINER function,
fixed `search_path=pg_catalog,scheduler_private`, fully qualified objects, no
dynamic SQL, lock_timeout250ms, READ COMMITTED transaction<=2s, fresh post-wait
clock_timestamp() plus durable time floor. No new role attributes, table/column
rights, secret reads, CREATE/TEMP or other EXECUTE.

```sql
ALTER FUNCTION scheduler_private.recover_started(
 uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text
) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.recover_started(
 uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text
) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.recover_started(
 uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text
) TO devpilot_scheduler_controller;
```

These are the exact approved statements; implementation and tests follow this boundary.
Worker, Supervisor, API and PUBLIC get no new EXECUTE. Migration1–7 stay unchanged.

## Strict behavior and trust boundary

Authenticate actual session_user and its registered live Controller incarnation,
not a boolean parameter or a user Session. Recovery works from a known complete
Run identity already held by the fixture's trusted control/Supervisor registry.
It does not resurrect the original Worker or grant it a new incarnation.

Lock Workspace -> Project -> Monitor -> Activation -> Schedule -> Run -> existing
Budgets(global/W/P/M, sorted origin/IP keys) -> Reservation -> control, as the D6
recovery suborder. Never acquire User/Session/Membership in reverse. All waits
precede a fresh SQL clock/identity/lineage check. Recovery of old audit/quarantine
does not require the old Owner, authorization or Worker still to be active.

1. Match full scope, original v/g/epoch/fence, namespace and Supervisor incarnation.
   An invalid or mixed identity refuses. No leased->started, no renewal, no input
   capability, target permit, URL readback or HTTP observation can be produced.
2. For unresolved started Runs, mark `unknown` with one finished_at and only an
   existing fixed control stop_reason. Causes are fixed codes (lease_expired,
   worker_lost, commit_unknown, emergency_stop or egress_unavailable); lease_expired
   requires the actual lease to have expired after all waits. Preserve a preexisting
   fixed stop_reason. Nonreleased reservations become/stay quarantined and every
   physical slot/cooldown/token stays held. No rate refund or physical release.
3. `canceled` is allowed only as the first terminal classification for a known
   permit-denial control outcome: the single input was consumed, durable monotonic
   target authorization fields/keys show **no committed target permit ever**, and
   an already consumed trusted Supervisor receipt proves full matching physical
   stop. The approved6.3 protocol installs target access only after confirmed DB
   target reservation; NULL alone, elapsed time, process exit or Worker claims are
   insufficient. Real kernel/receiver/connect/Supervisor evidence must corroborate
   zero target TCP and no installed target permit. Target ACK uncertainty with a
   committed target_permit_id remains unknown even if no GET was seen. Start ACK
   uncertainty, loss of input confirmation or incomplete proof defaults to unknown.
   No `no_send=true` assertion parameter or new attestation trust is introduced.
4. Already unknown remains unknown forever, including when later proof establishes
   physical stop or a late Worker presents an otherwise valid observation. Never
   rewrite terminal HTTP fields. Existing completed facts win a race and are returned
   as already terminal; recovery cannot turn a committed result into unknown.
5. Only an already trusted, committed full physical release allows a single FUTURE
   recovery cycle for unknown/canceled. In the same transaction, and only while
   current configuration/activation/schedule v/g/epoch, live authorization, active
   Owner existence, deployment/HTTP readiness/stop and disk gates still permit it,
   replace `next_run_id=old_run_id` with one new UUID and set
   `next_due=max(gate_now+interval,last_started+interval,next_start_not_before)`.
   Skip backlog. No change to authorized v/g/epoch, interval or cooldown. No actual
   Run/Claim/Start or budget reservation; those gates are independently rechecked
   by the existing functions at the real future due. Capacity unavailable at actual
   Claim/Start remains blocked. This is only recovery after7.3 unknown/canceled,
   not general completed-result scheduling or the later continuous-loop tasks.
6. A repeated call/unknown Commit must read the same terminal fact and existing
   next identity. Once next_run_id changes, it cannot generate another UUID, shift
   due forward, modify the new Run or send old input. A terminal result race or
   pause/PATCH/renewal never overwrites current observations or a newer schedule.
7. Return only bounded recovery identity/state and next-cycle timing/blocked fact;
   no URL, DNS/IP/HMAC key, user secret, raw error or HTTP result payload. No new
   execution-status endpoint, history API or retention implementation.

The Controller is trusted to conservatively abandon unresolved work; a false loss
signal may lose availability, never create a target result or prove physical
release. Physical receipt acceptance and Ed25519/Linux proof verification remain
exclusively the existing independently approved Supervisor chain. No administrator
UPDATE, mock proof, Lease expiry or Docker deletion may substitute for it.

## Required real acceptance after approval

| Requirement/scenario | Real test evidence planned (not yet executed) |
| --- | --- |
| D5/D8; monitor-runs Unknown delivery/Process dies after GET | Real fixture GET followed by worker/control loss; restricted Controller transitions unknown; zero HTTP result fields; no same-Run resend; quarantine until actual independent stop |
| Start ACK lost; input/target ACK ambiguity | Actual PG Commit then discarded ACK; no reconstructed pipe; unknown remains terminal; target permit row versus no-permit proof is distinguished |
| Unknown Commit and partition | Real Rollback, real Commit/lost ACK and unavailable DB; same known Run recovery is idempotent; no false rollback inference or repeated GET |
| Lease expires with live zombie | Real45s lease wait, still-live isolated process; unknown/quarantine retains32/4/2/1 and target slots; independent stop then normal receipt consumption is the only refund |
| Known permit denial | Original kernel denial plus zero target TCP/GET, immutable no-target reservation and real signed termination; canceled has no fabricated HTTP failure |
| Late result / independent connection race | Genuine Worker commit_result races Controller recovery under Workspace barriers; pg_locks/pg_blocking_pids/time/SQLSTATE show exactly one terminal fact; old Worker/Fence cannot overwrite unknown |
| Later normal cycle | Actual minimum60s interval after trusted physical recovery, fresh Run/fence/namespace, new confirmed Start/permit/GET/physical release; original unknown never resent; no three-cycle or7.4+ implementation |
| Pause/PATCH/expiry/Owner unavailable | Recovery of old audit still safe; current permission mismatch prohibits arming a future cycle; fresh Claim/Start enforces original Owner parent order |
| Role/scope/sentinel/budget | Real LOGIN, cross-tenant/fence/incarnation denial, immutable resource ledger, no sensitive data; normal/race, all historical safety regressions retained |

Freeze source/commit and record real commands, exit codes, Linux/PG18 environment,
logs and artifacts before claiming completion. Preserve every existing negative
record and the e767a5a/2f00bf7 independent evidence. Keep7.3 unchecked until acceptance.

Approval status: **explicitly approved**. Original source: this conversation,
request_user_input_async `call_377fc6b7f666486fa1546ea9dc9660dc`, question0.
The full signature, sole Controller grantee and trust boundary above were presented;
the product owner replied verbatim: “批准仅这一项窄 EXECUTE及所列信任边界”.
This approval is specific to Task7.3 and is not inferred from earlier approvals.
At the approval boundary only this document and verification.md had changed; no
SQL/Go/permission/test assertion or new fixture run preceded approval.
