# Task7.1 minimum result persistence — separately approved capability

Baseline:65316cc41e7e4a1186e8ce3a7d2d53a937e6380d, PR11 Open.
The current human request accepts6.3 (29/47 after the checkbox update) and
allows only7.1. It explicitly requires separate permission before new database
capabilities. The separately approved implementation is limited to the capability below.

Existing `devpilot_scheduler_worker` can Claim/Start/reserve a target, but has no
result-writing function or direct monitor_runs UPDATE. Existing Controller and
Supervisor functions intentionally cannot write HTTP results. Reusing one to
smuggle result writes, granting table UPDATE or using a fixture superuser as the
writer would breach the accepted boundary. One precise Worker EXECUTE is needed.

## Exact signature and sole grantee

```sql
scheduler_private.persist_minimal_result(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_v bigint, p_g bigint, p_epoch bigint, p_fence bigint,
 p_namespace uuid, p_supervisor uuid,
 p_kind text,
 p_duration_ms integer, p_http_status integer, p_success boolean,
 p_failure_code text, p_stop_reason text
) RETURNS jsonb
```

Exact approved ACL difference:

```sql
ALTER FUNCTION scheduler_private.persist_minimal_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text,integer,integer,boolean,text,text) OWNER TO devpilot_scheduler_owner;
REVOKE ALL ON FUNCTION scheduler_private.persist_minimal_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text,integer,integer,boolean,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.persist_minimal_result(uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,uuid,uuid,text,integer,integer,boolean,text,text) TO devpilot_scheduler_worker;
```

No additional function/overload or helper capability is requested. The existing
non-LOGIN owner already has relevant Run/control-table privileges. Role
attributes/memberships, table/column ACLs, CREATE/TEMP, secrets and all other
function EXECUTE grants stay unchanged. Controller, Supervisor, API, legacy
roles and PUBLIC receive no corresponding EXECUTE. Migration1–7 remain exact;
the unmerged Migration8 function definition is tested only in fresh guardedPG18.

## Reporting trust and exact write boundary

The authenticated Worker is trusted to report the minimum observation obtained
from the original sealed kernel/one-shot input. This grant cannot cryptographically
prove that a remote server sent an HTTP status. PostgreSQL validates caller
session_user and its registered incarnation, complete Run identity, state,
Lease/Fence and schema constraints. No caller-supplied `system=true` or
`verified=true` bypass, arbitrary SQL, URL, raw payload or error-text parameter.

The original kernel returns a nonnil Result only for an admitted observation;
configuration/admission/target-permit refusal returns nil Result/error instead.
The fixture-only Go adapter will accept those distinct typed outcomes, strictly
validate allowed fields and lineage, and never derive failure_code from raw
error strings. The result writer accepts only a typed original-kernel Result, not arbitrary
JSON or an error-text payload. Invalid typed values and enum sentinels reject
before a database call; unknown payload fields cannot enter this interface.
Errors/log formatting are fixed and redacted.
The SQL function independently checks numeric/enum/null combinations.

`p_kind` is exactly one of `network`, `preflight`, `permit_denied`, or
`physical_stop_unconfirmed`. It is a bounded validation discriminator, not a new
persisted detail column, public DTO or user-selectable execution operation.

- **network:** only an admitted original-kernel observation. Full v/g/epoch/fence
  matches the immutable started Run and the current valid original Worker
  incarnation/Lease. Namespace/Supervisor match its frozen Reservation. The
  Reservation must already be released by the independently accepted Supervisor
  receipt, with matching physical-release stamp and immutable receipt metadata.
  Only then may one started→completed write set bounded duration0..30000,
  nullable final status200..599, success, fixed failure_code and DB finish time.
  DNS/IP/network failures use only the original nine codes; `invalid_configuration`
  is a control stop, not a fabricated HTTP failure. A genuine DNS-stage failure
  may precede target reservation; a refused Target Permit must never be converted
  to connect_failed/timeout/success. Structural checks cannot replace the trusted
  adapter's original Result/error distinction.
- **preflight:** existing owned Run only; no new Run is created. No target/input
  capability may have been consumed inconsistently with that phase. Store only
  an allowed fixed stop_reason; duration/status/success/failure_code remain NULL.
  If no Run exists, rejection stays an in-memory control outcome, no fake row.
- **permit_denied:** an owned confirmed Start/input with refusal or uncertain
  target permission. Store only fixed stop_reason. A committed target budget is
  not evidence that Supervisor confirmed permission or that any TCP occurred.
- **physical_stop_unconfirmed:** no confirmed physical release, regardless of
  whether an in-memory HTTP Result exists. Store only fixed stop_reason; preserve
  all physical slots/reservation state/unknownness and all HTTP fields NULL.
  Successful observation and physical-stop failure are separate facts. No
  success=false/timeout is manufactured to represent unavailable termination.

All control-only writes leave attempt state and finish time unchanged, avoiding
advance implementation of7.3's canceled/unknown recovery. stop_reason is a
single-assignment fixed enum. A duplicate or conflicting message rejects.
Network-result writes reject a preexisting control outcome rather than silently
replace it. Existing terminal/unknown runs, obsolete fences/incarnations,
foreign/mixed scopes, wrong generations and invalid result data reject atomically.

The function never changes physical_release_confirmed_at, reservation/receipt,
resource or rate counters, network permission, Activation, authorization,
Schedule next_due/cooling/last/latest/current pointers, or any configuration.
The Claim already reserves bounded Run-retention capacity; this write consumes
no new Run/capacity and cannot unblock or schedule a future cycle.

## Task7.2 is not included

Schema8's existing CHECK requires a nonnull `result_commit_id` for a completed
row. To preserve that CHECK, the function would create an internal opaque UUID
only during its single first write. Callers cannot choose it; it is not returned
as a retry key, and there is no reconcile/readback/idempotent resubmission
function or automatic retry. The Go adapter burns its single result-submission
capability even on unknown Commit and returns only fixed uncertainty. Unknown
Commit is not treated as rollback. The fixed caller-owned commit_id/digest and
idempotent reconciliation protocol remain7.2 and are not implemented here.

Existing `execution_status` already reads completed Run rows and filters its
provisional current_observation by v/g/epoch. It will naturally see a newly
persisted completed row. This request does not modify that existing query,
create new projection logic, update Schedule pointers or claim7.2/7.4/7.5
acceptance. It does not silently hide or alter an existing status contract.

As approved in DesignD6, Result uses the Run's original valid lineage, not a
new authorization for execution. Pause/PATCH do not relabel that Run: a valid
original result may remain historical audit, and current mismatched lineage
never gains a pointer update. Obsolete/unknown/terminal Run or expired Lease,
old Fence or incarnation are refused. User/Owner/Session loss is not a reason
for reverse-locking identity during recovery. The existing Design distinction
between valid historical Run identity and a superseded/invalid Run is preserved.

## Locks, budgets and proposed real acceptance

Use the existing recovery suborder: Workspace → Project → Monitor → Activation
→ Schedule → Run → ordered Budgets → Reservation → control/registered identity.
No late User/Session/Membership locks or business connection across Probe I/O,
Supervisor stop, signature verification or proof-journal work. Function is
VOLATILE PL/pgSQL SECURITY DEFINER, fixed trusted search_path, qualified objects,
no dynamicSQL, lock_timeout250ms, transaction/statement budgets≤2s. Final fresh
SPI command after all waits revalidates identity/Run lineage/Lease and saves
one clock_timestamp for DML. Existing deployment stop never grants a new
execution; the result function cannot reopen a gate.

After explicit capability approval, actual guardedPG18 normal/race will verify:
original successful/failed observation and nullable combinations; all four
control phases; physical-stop proof required before network persistence;
wrong scope/v/g/epoch/fence/namespace/incarnation, terminal/unknown/expired Run;
two independent writers for one result and control/result races; rollback and
unknown Commit without resend; SQLSTATE/locks/barriers and unchanged resource
ledger; sentinels in URL/DNS/TLS/header/body/raw error/Cookie/credentials cannot
reach Run fields, returned DTO or ordinary logs. Real Linux6.3 execution and
5.4 accepted receipt supply the positive end-to-end physical precondition; no
admin UPDATE or mocked Supervisor response substitutes for that proof.

Retain6.3 joint46+6, Budget23×2, Snapshot4×2, PG147×2, original81,6.4/6.5/5.4,
API zero-target, unit/race/vet and Change strict. Only actually passed7.1 tests
permit a checkbox update. No7.2, next cycle, production entrance, deployment,
merge or Archive is authorized by this capability request.

## Original human authorization

Source: this Codex conversation, asynchronous permission reply to
`call_965917754402442daac6333bf80e0312` (question item index0). This is the
7.1-specific request quoted by the human reply, not any earlier Claim, target
budget or Supervisor authorization. Exact reply:

> 批准仅这一项窄 EXECUTE及所列信任边界

The question specified the complete17-argument signature above, Worker-only
EXECUTE, original-kernel reporting trust, prior trusted physical release for
HTTP results, control-only stop reasons, internal/nonreturned result UUID, and
no7.2 projection/idempotency/scheduling. Its full boundary is retained above.
The conversation has no independent external message URL; no external review
or timestamp is inferred from this local transcription. This permits only the
stated implementation and tests; acceptance still requires actual new evidence.
