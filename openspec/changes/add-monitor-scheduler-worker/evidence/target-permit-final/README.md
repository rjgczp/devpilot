# Task6.3 partial implementation review evidence

Reviewed starting HEAD: `0fd6502f30c0882ae083773feb7f24b8fe41a524`.
Frozen implementation/test SHA: **`8593edccd94e1ed00348e5cb0a98895f1b71cb71`**.
Source inventory SHA256: `fb92054560fd6af9e348f147ab336f4f97bc6d0e7b40edd2489e3f35288b08bf`.
Final receipts are clean at that SHA. Subsequent delivery only changes
verification/evidence; task checkboxes, implementation and tests remain unchanged.
Tests ran October21–22,2026 against the same frozen bytes. No PR/merge/deploy.

**6.3 remains incomplete. Total25/47.** This package proves the authorized
default-refusal hook and permit protocol portion only. 5.4/6.4/6.5 remain
unchecked. Production execution is hard closed. There is no independent
network-capable scheduler Probe or concrete Supervisor installer in this batch.
See `tests/scheduler/target-permit.md` for implementation/trust boundaries.

## Actual results

| Group | Actual result |
| --- | --- |
| PG18 target/Start/budget normal and race | 23/23 each, exit0, no skip |
| Historical PG18 integration and integration-race | 147 each, exit0, no skip; original145+real60s authorization+real45s Lease partitions |
| Extra request-expiry lock barrier | normal/race exit0; server raw SQLSTATE57014; only log verbosity increased, same permissions/timeouts |
| Original isolated network kernel | 76 scenarios passed, including deliberate bypass controls detected as expected audit failures |
| Added target-permit refusal controls | 5 passed; each real DNS, zero target connect attempts |
| Default Go unit/race/vet and Linux fixture unit/race/vet | exit0, count1, no network in Linux tool container |
| gofmt, git diff check, Migration1–8 byte/hash guards | exit0; all migrations unchanged vs reviewed starting HEAD; no new SQL or ACL |
| Change strict | exit0 |
| Whole repository strict | exit1, exactly original4 specs/7 length warnings; semantic path comparison unchanged |
| Final original protected PG18 | schema0, no scheduler roles/tables/other transactions/lock waits |

## How to inspect

`manifest.json` records delivered-byte and uncompressed SHA256 for every listed
file. Compressed files preserve original bytes; use `gzip -dc <file>.gz`.
Receipts containing large source inventories are `.json.gz`; their commands,
exit code, actual HEAD/dirty, source hashes and binary hashes remain intact.
Sibling logs are `.log.gz`; receipt `log_sha256` hashes the uncompressed log.
No executable binaries are committed. The verbose-only supplemental harness is
included with its hash; create commands prove that only PG log verbosity differs.

- `pg-{normal,race}-coverage.json`: exact23 tests, commands and labeled disposable
  server identities. Per-test receipts, PG logs, inspect and held ledgers share
  the same prefix. No test fakes physical-release timestamps/termination receipts.
- `history-{normal,race}-coverage.json`: exact147 historical tests; three raw
  command receipts/logs per mode. The largest race partition took117.24s under
  its unchanged120s limit; no timeout was relaxed.
- `verbose-{normal,race}-0-postgres.log.gz`: actual cancellation SQLSTATE57014.
  Corresponding logs contain pg_locks/pg_blocking_pids and held final states.
- `network/summary.json`: original76 plus5 new cases. Original4 deliberate bypass
  controls require child_exit0/audit_exit1; ordinary cases require audit_exit0.
  `permit-*` require actual DNS and target_attempts0. Trace, independent receiver,
  routes, firewall, resource and build/binary evidence are retained there.
- `requirement-test-evidence.json`: Requirement/Scenario/Test/Evidence index.
- `source-guards.log.gz`, `toolchain.log.gz`, `final-pg-state.log.gz`: actual
  migrations/tool/digest/cleanup proofs. `strict-warning-comparison.json` compares
  paths, not nondeterministic validation duration fields.

The5 network refusal controls simulate callback failure/disconnection/unknown
outcomes at the original dial gate. Real PG commits, lost ACK and rollback are
separately exercised by TestBudgetTargetPipeCommitBoundary. These layers are
**not** claimed to be a combined Supervisor/PG/network topology. Protocol witnesses
only inspect committed reservation ordering and exact request/ack binding; no
witness installs firewall rules or signs physical termination proof. The original
kernel fixture's explicit local allow hook exists only in executionnet `_test.go`
and grants no scheduler execution permission.

## Retained failures and limits

`development/lease-timestamp/`: initial true PG run refused locally before target
SQL, because start_run omits Claim timestamps and the new adapter required them.
Start now retains the already-validated original Lease after confirmed commit;
SQL/roles/ACL unchanged. It cannot reconstruct or reissue a Start capability.

`development/initial-freeze/`: first network run failed its old concurrency
assertion when cancellation happened before target permission. The minimal test
fix accepts ErrTargetPermit only with canceled parent context and nil Result,
accounts for all32 admitted runs, retains original canceled-result, actual GET
witness and FD/admission recovery checks. Final original76+5 all pass. The same
folder preserves host unit failure caused by sandbox local-listen denial; the
explicitly approved local-test run and network-none Linux runs passed. No security
policy/timeout was weakened and no failure is counted as acceptance.

An automatic approval service usage limit prevented a read-only tool-version
command on October21; no command ran and it was not an unsafe-action judgement.
After the user's continuation, the normal approval path succeeded on October22.
Earlier Supervisor tool-approval timeout root cause remains unproved; production
historical-role ACL inventory is still unavailable. Neither authorizes inferred
production GRANTs. API target-network strace audits were not rerun in this batch;
their original exact whitelist and source guards are unchanged.

Remaining acceptance:6.4 per-Run default-deny namespace/veth/firewall and trusted
rule-install channel, independent credential-free network-capable Probe IPC,
6.5 physical termination, then5.4 safe physical release. No full scheduler GET,
production target, periodic cycle or physical stop is claimed. No advance to
6.4 is authorized by this package.
