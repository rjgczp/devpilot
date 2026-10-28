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

This is a 16-argument network-result-only entrypoint. Control stop reasons remain in the approved 7.1 function and do not receive a fabricated HTTP result or commit identity. The existing 17-argument persist_minimal_result signature and Worker-only grant remain; its network branch is retired by the separately authorized compatibility convergence below. Control outcomes keep their single-attempt semantics.

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

## Separately authorized legacy network convergence (2026-10-27)

After the 7.2 delivery at `2f00bf7db2634c6d0c2a69a28da511a879417e02`, source review found the same Worker role could still directly execute the old network branch. Static absence of a Go callsite was insufficient. The user asked “证明旧网络写入路径不会被新的 Worker 调用，或者提出兼容性收敛方案” and then explicitly instructed “按你的方案实施” after the proposed convergence. This authorization retires only the legacy network semantics; it does not expand any permission or authorize 7.3.

Current Migration8 rejects `persist_minimal_result(..., p_kind='network', ...)` with permanent SQLSTATE `0A000`, message `legacy_network_result_disabled`, after system identity and parameter validation and before business locks/DML. The signature/ACL, control phases and their gates are retained. Its old internal random-ID network write no longer exists. The Go compatibility method returns `ErrLegacyNetworkResult` before opening a transaction; the new `ResultWriter` interface exposes only typed control outcomes and fixed-ID Commit/Reconcile. This is a BREAKING internal fixture protocol change, not backward compatibility for old network callers. Direct SQL is also denied, even for a valid original Worker with genuine physical release proof.

No live in-place production upgrade is provided. For isolated fixture transition, stop new Starts, independently terminate and reconcile all physical capabilities before retiring the old disposable environment; then build a clean guarded schema8 from the current source. Never regenerate an ID for an already sent GET, auto-convert old completed records to new IDs, replay a Run or deploy a historical binary to a current DB. Existing migrated fixture databases do not automatically adopt an edited Migration8 via `goose up`; destroy/recreate only after trusted physical stop. Migration1–7 remain byte-identical. No production migration or deployment is authorized.

Historical 7.1 network semantics/assertions remain unchanged at `c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c`. `run-legacy-result71.py` creates a separate immutable checkout and runs that exact component's binaries, Go helpers, migrations and network harness in its own guarded, disposable schema8, normal/race (24 cells). Source/tree, toolchain/binary hashes and inner receipts explicitly identify the historical SHA; an outer current-SHA receipt binds orchestration. This is compatibility history, **not** evidence that current schema still accepts old network writes. Current-schema control-only tests (6 cells) and fixed-ID/legacy-refusal tests (18 cells) are reported separately. No historical assertion is weakened or relabeled as current implementation success.

Regression mapping: D6 Result Commit; monitor-runs “Results contain only bounded private observations” and “Result commits are fenced and can be reconciled without networking” / “Exception - Successful commit loses acknowledgement” and lineage scenarios; monitor-scheduling “Claim start and result have separate confirmed commit gates”. `TestResultLegacyNetworkDisabledPG` uses the original kernel's real HTTPS result and committed Supervisor receipt, invokes old SQL directly with the same Worker login, verifies exact refusal and unchanged full Run/control/resource/projection snapshots, and then succeeds through fixed-ID Commit/Reconcile without another Probe/GET. `TestLegacyNetworkWriterRefusesBeforeDatabase` proves the Go guard precedes DB access. Existing minimum-result sentinel, identity, ACL and physical-stop assertions remain in their proper component regression suites.

Independent review of this convergence and residual risks remains pending. All previous negative evidence and uncertain concurrency/timeout causes remain retained; green CI is not independent approval or permission to start 7.3.
