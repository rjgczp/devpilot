# Checkpoint3 approved narrow capabilities

Scope: OpenSpec tasks5.1–5.3. Task5.4 remains incomplete, with physical release
acceptance dependent on the independent Supervisor in group6. No production
execution, target networking, PR, deployment or archive is authorized here.
Implementation baseline:655d9ee9429bc3032eec0ae95246a6f3994afc5c.
Authorization/review baseline for this supplement:
**e56c69a5832e88dfaac469071f9ada51a758dadc**, independently reviewed
Checkpoint3 on branch `codex/add-monitor-scheduler-worker`.
The Checkpoint2 baseline is code history, not the source of these grants.

## Two runtime EXECUTE additions — explicitly approved

```sql
scheduler_private.reserve_target(
 p_incarnation uuid,
 p_wid uuid, p_pid uuid, p_mid uuid,
 p_uid uuid, p_membership uuid,
 p_run uuid, p_fence bigint,
 p_key_generation bigint,
 p_origin_key text, p_ip_key text
) RETURNS jsonb

scheduler_private.rotate_target_budget_keys(
 p_incarnation uuid,
 p_next_generation bigint,
 p_catalog jsonb
) RETURNS jsonb
```

| Capability | Proposed grantee | Bounded authority |
|---|---|---|
| reserve_target | devpilot_scheduler_worker | For one already-started scoped Run owned by this registered login/incarnation, reserve the approved fixture origin/IP budgets once. Check current lineage, Owner, authorization, Lease/Fence, consumed input, deployment/stop and registered catalog after waits. Return only permit identity/deadline; no network, Supervisor rule installation, input delivery, result or resource release. |
| rotate_target_budget_keys | devpilot_scheduler_controller | Initialize/rotate the bounded trusted fixture digest catalog under global budget/control locks, only after all active/quarantine concurrency is drained. Retain old digest keys/cooldown at least1h and use retained generations together so new keys do not reset rate. Cannot open deployment gate, alter user/monitor authorization, mint a Run, stop a process or release a reservation. |

The catalog contains only opaque fixture-entry identities, key generations and
HMAC digest pairs; no URL/path, raw DNS/IP or HMAC secret is stored. A bounded
control-row catalog uses the existing private owner's scheduler_control access;
no extra table/column grants or database role attributes are requested.

The Worker supplies the digest pair from the control-plane HMAC/catalog adapter.
The DB validates it against the trusted catalog and locks all retained origin
keys, then all retained IP keys in deterministic order. Probe never receives
HMAC material or DB access. Fixture catalog membership is checked offline; no
DNS or target TCP is performed by this checkpoint.

For target reservation, use the existing Owner User→W→Membership→P→M→Activation→
Schedule→Run order, followed by global/W/P/M budgets, sorted origin then IP keys,
Reservation and final control protection. All waits precede fresh authorization
and clock gates. Target capacity or rate refusal rolls back the whole target
transaction; committed Start tokens are not refunded. Repeated/uncertain permit
requests cannot mint a second permit or execution capability.

Rotation takes the global budget lock before target keys/control and never then
acquires a User or scoped parent. It retains existing catalog entries/generation
mappings and old budget cooling; it cannot accept a replacement catalog that
silently discards retained keys. A pending old generation prevents another
rotation until its safety retention has elapsed. No raw key is sent to SQL.

Approved ACL difference:

```sql
REVOKE ALL ON FUNCTION scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text) TO devpilot_scheduler_worker;
REVOKE ALL ON FUNCTION scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb) TO devpilot_scheduler_controller;
```

Both implemented functions are VOLATILE PL/pgSQL SECURITY DEFINER, owned by the existing
non-LOGIN private owner, with fixed trusted search_path, fully qualified objects,
no dynamic SQL, no PUBLIC execute and existing2s/250ms budgets. Other runtime
roles receive no execute. Existing API/ordinary build boundaries remain.

## Authorization provenance — current product-owner supplement

The current human request, “Checkpoint 3 已通过独立技术评审，并获产品负责人正式补充授权”,
explicitly approves exactly:

- `scheduler_private.reserve_target(uuid,uuid,uuid,uuid,uuid,uuid,uuid,bigint,bigint,text,text)`: EXECUTE only for `devpilot_scheduler_worker`.
- `scheduler_private.rotate_target_budget_keys(uuid,bigint,jsonb)`: EXECUTE only for `devpilot_scheduler_controller`.

This supplement follows review of **e56c69a5832e88dfaac469071f9ada51a758dadc**.
It is the authoritative product-owner authorization recorded in this document.
The earlier short confirmation “批准仅这两项窄 EXECUTE” is historical conversation
context; no Checkpoint2 authorization is substituted for this supplement.
Checkpoint2's reconcile_claim/expire_unstarted grants are separate capabilities.
This documentation correction changes no SQL, grant, role attribute, table/column
ACL or secret privilege. It grants no physical-release capability. Acceptance
is independently recorded in verification.md; authorization is not test evidence.

## Task5.4 boundary

No Supervisor termination receipt is manufactured for budget acceptance. A
physical stop protocol must bind the complete Run lineage, Supervisor and
namespace incarnations, irreversible permit revocation, established-flow cut,
process exit and non-recoverability. Until independent group6 proof exists,
started/unknown slots and target reservations remain quarantined. SQL fixtures
or a successful database update cannot substitute for that proof; task5.4 must
remain unchecked. Test teardown is not physical-release acceptance.
