# Checkpoint2 independent review evidence

Accepted scope: tasks4.1–4.6 only. Completed20/47; Checkpoint3 and later remain unimplemented/unaccepted. No PR, merge, Archive, deployment or production migration. Production target execution stays hard closed.

Frozen implementation/test SHA: `6eb76fc93e86d7cc9038f86b74bfbd6f03b4b877`.
All final receipts below record clean=true (dirty=false) and the same source inventory
`c93642d57c7469533da7b9d9fa801a255d160fa80e48be14efc13e07a2d9112a`.
Subsequent review-delivery commit changes task checkboxes/evidence/verification only;
[implementation binding](implementation-binding.json) verifies every implementation/test byte.

| Actual command receipt | Result |
|---|---|
| [PG normal coverage](pg-normal-coverage.json), pg-normal-0/1/2.json |147/147, three complete serial partitions145+1+1; no skip |
| [PG race coverage](pg-race-coverage.json), pg-race-0/1/2.json |147/147, identical inventory and partition coverage; no skip |
| host-unit.json / host-race.json |Full Go unit/race, count=1, exit0 |
| linux-fixture-unit.json / linux-fixture-race.json |Full Linux fixture unit/race, count=1, exit0, network none |
| static.json / linux-fixture-vet.json |gofmt, vet(default and fixture), diff-check, M1–7 hashes, exit0 |
| change-strict.json |exit0 |
| repository-strict.json |exit1, exactly unchanged4 historical specs/7 description-length warnings |
| final-pg-state.json/.log |PG18.6, schema0, no Schedule/temporary roles/other transactions/lock waits |

The two real deadlines have independent processes:60s authorization and the original45s Lease.
Every other test retains the original120s process limit. SQL functions retain2s transaction/
statement and250ms lock limits. The injected55P03/57014/40P01 are expected failure-mode tests,
not unexpected safety timeouts or normal-protocol deadlocks.

[Requirement → Scenario → files → tests → exact command → evidence → SHA](requirement-test-evidence.json)
and [per-task CLI completion records](task-completion-record.json) provide the six task mappings.
[Normal outcomes](outcomes-normal.txt) / [race outcomes](outcomes-race.txt) make SQLSTATEs and
configuration versions readable. `protocol-events-{normal,race}.jsonl.gz` preserve extracted
raw pg_locks/pg_blocking_pids, barriers, DB times and final states, with exact source line references.
Full logs are `pg-{normal,race}-0.log.gz`, plus uncompressed deadline logs1/2.
`postgres-log.log.gz` is the original guarded fixture server log, including deadlock/error diagnostics.
Commit ACK observation is an upper bound, not an exact server commit timestamp.

Each compact receipt preserves the original command, SHA, dirty flag, binary/log hashes and exit code.
Its `original_receipt_ref` is the original, unmodified JSON bytes compressed with gzip; verify with
`original_receipt_sha256`. Shared source inventories avoid duplicating large JSON dictionaries.
For a compressed log, hash its decompressed bytes against receipt.log_sha256. `manifest.json`
hashes all delivered artifact bytes. No test binaries or large network traces are committed.

## Implementation and limits

The unique Owner→W→Membership→P→M→A→S→Run→Budgets→Reservation sequence is exercised using
independent real LOGIN connections, real user PATCH and explicit barriers/NOWAIT probes. Cases
include same/different Owners, same/different Monitor/Workspace candidates, five Owner loss modes
in both orders, Scope/membership identity revalidation, skip lock release, final fresh stop and
expiry gates, cancellation, timeout, and externally injected administrative deadlock rollback.
A65W/73M fixture proves64W/8M bounds, cursor rotation and32 claims/one successful claim per W.
This is not the later1000W/24h/network stress acceptance.

Claim reconciliation only returns the original current leased scope/incarnation. Both actual commit
then lost ACK and actual rollback then lost ACK are exercised. It cannot reconstruct input or retry
started work. Four real45s Lease cases cover never-prepared, PATCH-revoked, prepared and started.
Only the database-proven never-prepared cases return slots; prepared resources remain quarantined,
started is rejected, and repeat/stale-fence expiry cannot double-return capacity. Old configuration
recovery cannot schedule the revoked lineage. No target was contacted by these database cases.

`release_basis=never_prepared` explicitly differs from a Supervisor termination receipt: every DB
preparation/namespace/input/target field must be null, the Run is still leased with no Start, the
Lease has expired, and the non-LOGIN function owner executes the narrow operation. A namespace
preparation, even without Start, prevents this path. This is a database authority proof, not Linux
process/namespace termination evidence. Actual allocation/permit lifecycle and independent physical
termination remain mandatory Checkpoint3/later work; this checkpoint gives no physical proof.

Only the two user-explicitly approved EXECUTE capabilities were added. No runtime role attributes,
table/column ACL, secret-reading privileges or historical Migration1–7 changed. Existing API user
identity and pause semantics remain. See [protocol/state table](../../../../../tests/scheduler/checkpoint2.md)
and Migration8 diff against the reviewed8ebba60 baseline. Full Migration8 lifecycle and CP1 Owner/
Session/API contracts were re-executed in the147-test normal/race inventories.

## Development failures retained

`development/recovery/recovery-normal.*`: business assertions passed, but old fixture cleanup tried
rewriting an expired terminal state/time; the guard correctly rejected23514 and Down refused.
The fixture now preserves terminal state/finished time. `development/interim/checkpoint2-normal.*`:
small simulated clock regression correctly persisted max time; cleanup used earlier DB now and hit
23514. Fixture cleanup now uses max(DB time,claimed,started), without changing database protections.
A guarded, one-off administrative cleanup source/log is retained under development/recovery; it is
not a permanent test that could fabricate termination proof for future physical tests. Development
and first-freeze receipts retain their real SHA/dirty/hash and are not relabelled as final passes.
No unexpected security timeout, privilege inconsistency or normal-protocol lock-order conflict was
observed. The earlier CP1 Supervisor tool-review timeout's internal cause remains unproved.

Historical production role ACLs still have no real inventory; no production GRANT is inferred.
No API strace zero-target audit or original76 network scenarios are claimed as newly executed at this
SHA. They remain historical CP1/kernel evidence pending the final approved network acceptance;
ordinary Linux release boundaries and real PG API regressions do pass at this SHA. No Supervisor,
Probe, target-permit, periodic runner, heartbeat/result/physical recovery or disk/retention loop is
claimed complete here. These are later tasks, not substitutes based on database state.

Existing CI workflow's push branch list excludes this branch; no PR is created, so this checkpoint
has no new GitHub Actions CI conclusion. CI integration of fixture tests remains in later scope.
Review is based on the exact local isolated Linux/PG18 receipts above, not claimed remote green CI.
