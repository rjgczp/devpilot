# Schema7 legacy PATCH permission investigation

## Scope and conclusion

User authorized independent diagnosis and a necessary minimal fixture repair only. Progress remains **7/47**; Checkpoint1 is incomplete. No Checkpoint2 work, production implementation or migration changes, PR, push, merge, deployment or production probe occurred.

The failing `patch-first` role profile was an incomplete test fixture. The same LOGIN, grants and unmodified configuration Store fail on reconstructed Schema7 **before Migration8**. A nine-column INSERT grant, derived directly from the unchanged INSERT target list, makes the same real PATCH commit on Schema7 and Schema8. Migration8 does not remove or add this legacy principal's table/column/function privileges. This establishes the cause of this test failure; it is not an inventory or endorsement of a deployed production role.

## Raw failure and identity

- Original failure: `../checkpoint1-remaining/pg-migration-interleave.postgres.log`, backend3034, 2026-10-20 07:31:37.810 UTC. Its original receipt remains exit1.
- New independent Schema7 reproduction: `schema7-original.{json,log}`. The diagnostic command exits0 because it asserts the real **failed** PATCH; this is not a successful PATCH result.
- Final reproduction in `targeted-regression.log`: backend3157, session_user=current_user=`checkpoint1_legacy`, READ COMMITTED. Driver error **42501**, `permission denied for table http_monitors`, routine=`aclcheck_error`, position0, empty WHERE/internal query/schema/table fields. The exact prepared SQL, without arguments, is in this log. Credentials, session digest and URL arguments are not logged by the wrapper.
- Object owner=`devpilot_fixture`; legacy LOGIN is not owner, has no memberships, superuser, BYPASSRLS, CREATEROLE, CREATEDB or REPLICATION. Catalog snapshots include ownership, raw/effective table and column ACLs, function EXECUTE, schema/database rights, triggers, FKs and defaults.

The failing statement is the production `internal/monitor/store.go` `writeSQL`: materialized identity/state/decision CTEs followed by `inserted AS (INSERT INTO http_monitors (...nine columns...) SELECT ... WHERE $7 AND result='ok')` and `changed AS (UPDATE ... WHERE NOT $7 ...)`, both `RETURNING *`. PATCH supplies `$7=false`, but the statement still requires INSERT privileges for its explicitly listed columns. The denial is at the top-level privilege check, without a PL/pgSQL trigger/function error context. It is not a failure inside the new SECURITY DEFINER function. `schema7-source-proof.json` proves this Store and Migration1–7 byte-identical to the reviewed main baseline.

Schema7 trigger path for a successful PATCH: Monitor identity/version guards → AFTER UPDATE `monitor_activation_invalidate()` → Activation guard. These original functions are SECURITY INVOKER (legacy current_user), owned by the fixture migration administrator; existing Activation UPDATE permission is needed. Schema8 additionally invokes `scheduler_private.activation_revoke()` from the Activation UPDATE trigger, SECURITY DEFINER owned by NOLOGIN `devpilot_scheduler_owner`, fixed `search_path=pg_catalog,scheduler_private`, fully qualified Schedule DML. Its owner/definer/path are recorded from PG catalogs. Legacy has neither private-schema USAGE nor EXECUTE on any private function. Actual successful old-role PATCH nevertheless clears a separately created authorization and advances epoch, proving this trigger path works without granting the caller the definer's capability. We did not instrument/replace these production functions.

## Proven baseline versus unavailable deployment history

An independent guarded 0→7 reconstruction created all historical business objects from untouched migrations. Before the correction, `checkpoint1_legacy` had exactly the original failed test's USAGE plus SELECT/UPDATE profile. It failed the same Store PATCH; after only the column grant below it committed version1→2. The same backend3161 remained the login for the Schema8 PATCH, committing version2→3 and revoking an existing Schedule grant. No role switch or owner impersonation was used for either PATCH.

No deployment LOGIN/GRANT provisioning record exists in Migration1–7 or the reviewed repository artifacts. We therefore cannot assert what any real production historical role possesses. The test role is a scoped simulation of the unchanged Schema7 PATCH dependencies, not a reproduction of a documented production credential. This is sufficient to identify and repair the demonstrably invalid fixture; it does **not** justify changing production grants. The reused disposable database retains database/schema-level PUBLIC restrictions from prior tests; those are explicitly captured. It is not claimed to be a pristine new cluster. Monitor/Activation tables and their original functions were recreated from 0, and the explicit legacy grants are recorded.

## Minimal repair

Only `TestCheckpointOneOldWritersStraddleMigration` now additionally executes:

```sql
GRANT INSERT(workspace_id,project_id,name,url,method,interval_seconds,
             timeout_seconds,expected_status,created_by_user_id)
ON public.http_monitors TO checkpoint1_legacy;
```

The grant exists only in the disposable test fixture. It adds no table-wide INSERT privilege, id/version/timestamp INSERT privilege, role membership, elevation, DDL, new full-table write privilege or access to scheduler objects. The pre-existing historical fixture SELECT/UPDATE grants are unchanged; this investigation does not claim they are a minimal production API role profile. No Migration8 fix is needed or proposed. The Activation-only `scheduler_test_legacy` fixture remains unchanged.

Negative controls actually return42501 for Schedule/Run reads and writes, authorize/claim function calls, and INSERT into the ungranted Monitor id column. The catalog assertion separately requires EXECUTE=false on **every** scheduler_private function, even though lack of schema USAGE alone would already reject calls. No new Run is created. A real authorization is created using the existing production Store through the bootstrap fixture connection, not through the legacy login; its private authorization function executes with its approved API function owner. Old-role PATCH then revokes it and cannot recreate it.

`permission-comparison.json` compares the old principal and PUBLIC ACL entries plus effective privilege booleans and owners for all original tables/columns/functions: zero differences across7→8. Full raw ACLs are retained in `*.acl.json`; Migration8 legitimately adds distinct scheduler-owner principals. Database/schema changes are recorded separately.

## Actual commands and results

All commands use the original container `db0774ff…`, fixed PG18 image, internal control network, no new capabilities/ports/privileges and the existing testdb guard. `environment.json` and `collect-environment.py` record the environment inspection. Native Goose3.26.0 is checked by every test. API DB budgets2s and existing locks/migration timeouts are unchanged; the targeted test process retains90s and fail-fast. Every receipt has full command, timestamps, source inventory, binary SHA256 and exit code.

1. `schema7-original.json`: original fixture on7, expected raw42501, diagnostic exit0, 0.33s.
2. `column-fix-baseline.json`: corrected Schema7 PATCH succeeded; diagnostic then exited1 because its first comparison incorrectly compared **all principals' ACLs**. Log retained. This was an assertion implementation error, not an observed legacy privilege mismatch, timeout or deadlock. The comparison now checks legacy/PUBLIC directly and legacy effective rights, retaining all other ACL entries as evidence. No grant was added to address this assertion failure.
3. `targeted-regression.json`: **11 top-level real PG18 tests passed**, exit0, 1.66s. Includes all three old-writer interleavings (patch-first included), unchanged original42501 negative control, corrected7/8 positive control, old Activation writer limitations, two Owners/same tag, Owner loss/rejoin cases, real HTTP auth/Owner/Member/ETag priorities, atomic authorization/revocation, Schedule failure rollback, all-tag migration retirement, repeat up/down/re-up, generation overflow and injected trigger/DDL rollback. Full per-subtest names, lock graphs and transaction barriers are in the log. This is targeted acceptance only, not the full47-task suite or universal lock-safety proof.
4. Build: `GOCACHE=/private/tmp/devpilot-scheduler-go-cache GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c -tags=integration -o /private/tmp/devpilot-scheduler-tools/legacy-permission-fixed.test ./tests/integration`, exit0; final binary hash in the receipt. The initial diagnostic binary has its own receipt/hash.
5. `static-checks.json`: exit1, Go's default host cache access was sandbox-denied, before vet could run. `static-checks-writable-cache.json`: same checks with existing writable `GOCACHE=/private/tmp/devpilot-scheduler-go-cache`, exit0: `go vet -tags=integration ./tests/integration`, Change strict, diff check. No test timeout or privilege bypass was involved. No full historical suite or Supervisor test was rerun.

`postgres-investigation.log` retains actual server failures and negative-control denials. `postflight.json`: PostgreSQL18.6, version0, no Schedule table, no temporary legacy roles, no other active transaction or lock wait. `review-code.diff` includes the three-line fixture change and the added diagnostic test only. Original failure evidence is not replaced or relabelled.

## SHA binding and limits

Runtime HEAD=`80ba9671ded3e0d0b022e5cf4cda8cfc14dbeabe`, dirty source tree explicitly recorded. Source inventory hashes bind the exact new test source, rather than claiming it was committed at test time. `code-source-binding.json` records the subsequent source commit and confirms equality of every source in the final passing receipt; later changes are evidence/verification documentation only. Production implementation remains byte-identical to that runtime HEAD; all Migration1–7 hashes are in `schema7-source-proof.json` and Migration8 is unchanged.

The earlier Supervisor tool-approval timeout still has no deterministic internal cause and remains a risk. This short successful run does not establish the absence of concurrency defects. Production execution stays hard closed. No tasks were newly checked. Stop here for independent review.
