# Task 7.2 narrow capabilities — separately approved

Baseline: c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c.
Date: 2026-10-27 (Asia/Shanghai).

The user accepted Task 7.1 technical review with all remaining risks and negative evidence retained, and authorized Task 7.2 implementation. This closes the implementation gate; it is not a claim that all risks have been eliminated. Previous explicit rules still require separate approval before expanding database function EXECUTE capabilities. The user separately approved exactly these two capabilities in the reply to call_d4ac3330cb3b4efaa42058b4153f9fec: “批准仅这两项窄 EXECUTE及所列信任边界”. Implementation and real acceptance remain separate.

## Approved exact capabilities

Only the existing devpilot_scheduler_worker role receives EXECUTE. Existing non-LOGIN devpilot_scheduler_owner owns each function; PUBLIC EXECUTE is revoked; Controller, Supervisor and API get no new capability. No role attributes, table/column ACLs, secret reads, CREATE/TEMP, or other EXECUTE changes.

### 1. Commit immutable network observation

```sql
scheduler_private.commit_result(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_v bigint, p_g bigint, p_epoch bigint, p_fence bigint,
 p_namespace uuid, p_supervisor uuid, p_commit_id uuid,
 p_duration_ms integer, p_http_status integer,
 p_success boolean, p_failure_code text
) RETURNS jsonb
```

This is a 16-argument network-result-only entrypoint. Control stop reasons remain in the approved 7.1 function and do not receive a fabricated HTTP result or commit identity. The existing 17-argument persist_minimal_result contract remains available with its existing single-attempt semantics and no expanded grant.

First write validates the original Run identity and fence, owning SQL login and Worker incarnation, current live Lease, system identity and trusted committed physical release proof. Parent locking retains Workspace → Project → Monitor → Activation → Schedule → Run → ordered Budgets → Reservation → control, with a post-wait fresh SQL time/identity check. No User/Session/Membership reverse locks.

Store the caller's nonnull stable UUID alongside the immutable minimal fields. For a previously committed result, the same Run, full lineage, original Worker identity, namespace/Supervisor incarnation, commit ID and exact nullable payload must match. Matching resubmission acknowledges the already stored fact without any row change, time change, projection rewrite or execution capability. A different ID or conflicting payload is rejected. A stale identity/fence cannot obtain a first-write permission. Identity checks still apply to readback; expiry or inability to establish authority must remain uncertainty/refusal, not new authority.

Current-observation projection is a separately conditional write in the same first-write transaction: update the existing latest_result_run_id only when current Monitor v, Activation g and Schedule epoch/authorized v/g still match the original Run. Pause, PATCH and reauthorization cannot let an old result overwrite current observation. No Schedule next_due, token refund, resource release, new Run, or next cycle. Existing read filtering must remain safe under lineage mismatch; no new HTTP status endpoint work.

### 2. Read-only unknown-Commit reconciliation

```sql
scheduler_private.reconcile_result(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid, p_run uuid,
 p_v bigint, p_g bigint, p_epoch bigint, p_fence bigint,
 p_namespace uuid, p_supervisor uuid, p_commit_id uuid
) RETURNS jsonb
```

This is a 12-argument scoped readback capability. Verify real SQL identity, original Worker/incarnation and complete Run lineage/namespace/Supervisor binding before returning bounded minimal committed fields for that Run and commit ID. Never enumerate other Runs or read configuration URL/secrets. Different known result IDs conflict; a missing visible commit is reported as not observed, not as proof that a concurrent/unknown transaction rolled back. Connection failure or authorization uncertainty is not converted to HTTP failure.

Reconciliation does not mutate Run, projection, resource/control ledgers or revive Lease/permit. Reading an existing committed fact need not create a fresh first-write Lease privilege. If committed, compare returned fixed fields against the pending immutable outcome before acknowledging it. If not observed, any explicit same-ID retry must still satisfy all first-write gates; there is never Probe invocation or network compensation.

## Exact approved GRANT delta (isolated Migration8 only)

```sql
GRANT EXECUTE ON FUNCTION scheduler_private.commit_result(
 uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,
 uuid,uuid,uuid,integer,integer,boolean,text
) TO devpilot_scheduler_worker;
GRANT EXECUTE ON FUNCTION scheduler_private.reconcile_result(
 uuid,uuid,uuid,uuid,uuid,bigint,bigint,bigint,bigint,
 uuid,uuid,uuid
) TO devpilot_scheduler_worker;
```

Both functions use fixed trusted search_path, fully qualified objects, no dynamic SQL, bounded READ COMMITTED transactions and existing lock/time budgets. No new direct SELECT/UPDATE grant to a login. Function owner uses only its existing table rights. No new Supervisor trust or execution capability.

## Trust and required evidence

The Worker remains the reporter of the original HTTP kernel's minimal observation; SQL does not cryptographically attest remote HTTP. Independent physical termination remains a prerequisite to first network-result persistence, and is never substituted by Result Commit. Fixed commit_id grants only same-result reconciliation, not repeated GET or physical permit.

Before marking 7.2 complete, guarded PG18 normal/race must exercise first commit, same-ID exact retry, conflicting payload, real Commit then lost ACK, real rollback and unresolved outcome, stale fence/Worker/incarnation, Pause/PATCH/reauthorization same-version lineage changes, independent writer races and cross-tenant denial. Preserve locks/SQLSTATE/timeline/actual receiver count and immutable release ledger evidence; retain existing 7.1 and execution safety regressions. No task7.3 lifecycle, task7.4 endpoint, task7.5 complete matrix or next cycle development.

Approval status: separately approved in this conversation by the exact reply quoted above. No broader role, table, secret or execution permission was approved.
