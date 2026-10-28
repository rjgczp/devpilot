# Task7.2 review and evidence index

## Latest convergence acceptance (2026-10-27)

User explicitly authorized the proposed retirement of legacy network semantics after source review at2f00bf7db2634c6d0c2a69a28da511a879417e02 found a direct SQL bypass of the new fixed-ID protocol. Scope/authorization/fixture transition: `monitor-scheduler-task72-capabilities.md`, final section. This is a BREAKING internal protocol narrowing; signature/ACL and control-only semantics stay intact. No new permission, production capability or7.3 work.

Frozen implementation58a7b834b11325dbf0b7ff97f630d670498f2908 (clean), source inventory8f41eaab3fcde55a4400d9aacb5ff2752ea58f1e2b31616fcb3d7beea961e4d8: current fixed-ID/legacy-refusal18/18, current control6/6 and immutable historical component24/24 all actually passed, normal/race, no retries/skips. All18 current joint receivers show one Header-only HTTPS GET, original kernel/Target Permit, complete physical stop and trusted DB release. Read-only raw evidence audit confirms every Run lineage and PIDFD exit. Local receipts and minimal raw witnesses: `openspec/changes/add-monitor-scheduler-worker/evidence/task72-convergence/`; full local raw paths in its index. Ordinary Go unit/race/vet, Linux fixture unit/race/vet, gofmt, Change strict and diff-check passed. Migration1–7 match main byte-for-byte; full-library strict exit1 remains the same4 historical specs/7 length warnings.

| Current requirement/scenario | Regression / real evidence |
| --- | --- |
| D6 fixed Result Commit; monitor-runs fenced/reconciled result | TestResultLegacyNetworkDisabledPG normal/race: genuine original Run and physical proof, same Worker direct old17SQL yields exact0A000; Go guard refuses before DB; complete Run/control/resource/projection snapshot unchanged; new fixed-ID commit/reconcile then succeeds without another GET |
| Control failures never become target HTTP failure | Original TestResultMinimalPG permit-missing/supervisor-killed and TestResultPreflightPG, current6cells normal/race, assertions unchanged |
| Unmodified old7.1 minimum-result contract | Exact c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c binaries/migrations/test helpers/harness in a separate guarded legacy-protocol schema8,24cells; inner receipts identify c1, not current code |
| Go network boundary is fixed-ID only | ResultWriter exposes typed control + Commit/Reconcile; TestLegacyNetworkWriterRefusesBeforeDatabase uses no DB and verifies the permanent legacy refusal does not consume the legitimate report |
| New implementation provenance vs historical component |59 actual command receipts bound to their component SHA/source/log/binary hashes:35 current (one documented full-library strict exit1) and24 historical exit0; source checkout contains old archived failures and is explicitly excluded from the newly executed receipt set |

Final delivery is a docs/evidence-only descendant of58a7b83. Actual Push/PR CI on that final SHA remains mandatory and will bind the current source again; do not relabel local58a7b83 or historicalc1 tests as the final SHA. CI retains147PG tests/mode,23Budget/mode,4Snapshot/mode, original76+5network tests,6.3/6.4/6.5/5.4 and exact API zero-target audit. A separate review package records actual final Actions results/artifact hashes after completion. Independent7.2/convergence approval remains pending; progress31/47, next7.3 not started. Historical negative evidence and unresolved root causes below are retained. No merge/archive/deployment/production execution.

The sections below record the earlier initial7.2 implementation; their16-case counts and pre-convergence17arg network behavior are historical, not the current protocol.


## Authority and baseline

2026-10-27 the product owner accepted the7.1 technical review while explicitly retaining unresolved risks/negative evidence, and authorized only7.2. Separately, reply to call_d4ac3330cb3b4efaa42058b4153f9fec approved exactly the two Worker EXECUTE capabilities and trust boundary in monitor-scheduler-task72-capabilities.md. No external independent7.2 signature is inferred.

Baseline c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c, main1d3b8266e494061beccc3ccdf47694ba4b0339c5. Migration1–7 bytes unchanged. This batch adds only7.2, retaining the17-argument7.1 function and all original safety regressions. No7.3 lifecycle,7.4HTTP endpoint,7.5full matrix, next cycle, merge/Archive/deployment or production networking.

## Implementation

- M8 adds commit_result16args and reconcile_result12args, owner unchanged/nonLOGIN, PUBLIC revoked, only Worker EXECUTE. No new role/table/column/secret permissions.
- commit_result validates immutable original Run scope/v/g/epoch/fence, SQL lease owner/incarnation, live Lease and independently accepted physical release before first write. Unique result_commit_id disallows cross-Run reuse. Result identity and fields become immutable together.
- Same ID + exact nullable payload acknowledges the stored result without rewriting time, control, projection or resources. A conflicting payload/ID, wrong Worker or lineage refuses. A replay grants no new first-write Lease permission.
- Projection updates the existing latest_result_run_id only when current Monitor v, Activation g and authorized v/g, Schedule epoch and current Run/Fence match. Old valid results can remain original audit after Pause/PATCH/same-version renewal; no next_due or resource settlement changes. Existing execution_status query/HTTP contract is unchanged.
- reconcile_result is scoped readback with original Worker identity and namespace/Supervisor binding. not_observed is a snapshot, not a rollback verdict. Unavailable/unauthorized queries remain errors. Go compares returned minimal payload with the frozen report before acknowledging it.
- Fixture-only result_commit_linux.go allocates a stable UUID once in NewResultCommit; immutable ResultCommit copies retain it. CommitResult and ReconcileResult have no Probe/permit invocation, automatic loop or network compensation. Caller must retain this report/ID for explicit retry; this does not implement restarted Worker recovery or7.3.
- The Worker remains a trusted reporter of the original kernel observation. SQL cannot attest remote HTTP; physical release still requires the independent Supervisor chain. No result code replaces physical proof.

## Requirement → Scenario → Test → Evidence

All rows concern monitor-runs “Result commits are fenced and can be reconciled without networking”, D5/D6 and the immutable private observation contract. Every cell uses fresh guarded PG18 plus real Linux HTTPS/physical proof, not a mocked release.

| Scenario | Test | Proof |
| --- | --- | --- |
| First result, exact fixed-ID retry and conflicting payload/ID | TestResultCommitValidationPG and all other commit cases | Completed row once; duplicate exact replay accepted; changed duration/ID refused; raw invalid/sensitive inputs rejected |
| Real Commit success, ACK discarded | TestResultCommitACKPG | Existing actual tx.Commit runs before io.ErrUnexpectedEOF; caller gets ErrCommitUnknown, scoped read confirms same fixed result, retry never repeats GET |
| Actual transaction rollback | TestResultCommitRollbackPG and validation rollback | Actual SQL succeeds, actual Rollback then unknown caller acknowledgment; read shows not_observed; explicit same-ID retry persists once with one GET |
| Old Fence/namespace/incarnation, mixed scope, wrong Worker | TestResultCommitValidationPG | P0001/42501/22023 as appropriate; real second Workspace/Project/Monitor with wrong Run; exact ACLs for both new functions |
| Pause before old completion | TestResultCommitPausePG | Real Owner activation command changes g/epoch; original result stored as old audit; current pointer not overwritten |
| PATCH before old completion | TestResultCommitPatchPG | Real Cookie-backed Owner Store PATCH changes v/g/epoch; old audit retained, current pointer not overwritten |
| Same v, new authorization generation | TestResultCommitRenewPG | Real60s cooldown before Claim, then real Owner renewal after physical release; original v same, g/epoch superseded; old result not projected |
| Two authenticated connections, same result ID | TestResultCommitRacePG | Workspace barrier, actual pg_locks/pg_blocking_pids, two commits acknowledge one row, no resource/projection mutation on retry |
| Original and foreign Worker race | TestResultCommitAuthorityPG | Two real SQL LOGIN/incarnations/independent connections; original writes, foreign identity refuses; no mixed ownership |
| No network/secret leakage or fabricated release | All eight cases and retained7.1 | Original GET/TLS/DNS/permit/physical proof/receiver assertions; one GET; result/control minimal fields and resource ledger unchanged by replay |

## Actual commands and evidence

Local command: python3 tests/scheduler/run-result72.py --tools /private/tmp/devpilot-result72/tools --evidence /private/tmp/devpilot-result72/acceptance-repaired. Eight normal plus eight race cells passed, all exit0, no retries/skips. Build uses the original separate legal tag combinations through build-result71.sh, CGO=0normal and CGO=1race. Actual record.py receipts preserve full argv, versions/binary/source/log hashes and parent SHA/dirty status. See openspec/changes/add-monitor-scheduler-worker/evidence/task72-local/index.json and manifest.json.

Local matrix source inventory9a231fce4721d8aea37efabe3144402146417d59f37f2f99efc77f998a117f9d is a development snapshot, not a clean-SHA claim. Full raw local trace/receiver/firewall/routes are under /private/tmp/devpilot-result72/acceptance-repaired. Bounded byte-preserved gzip excerpts are committed; large raw files are uploaded in final execution-security-evidence/result72-evidence. CI now executes run-result72.py after all original7.1/6.3/6.4/6.5/5.4/Budget/Snapshot/network steps, plus historical integration/race and API zero-target audits. Only the actual frozen Actions checkout receipts establish final SHA acceptance.

Native unit, race and vet passed in /private/tmp/devpilot-result72/checks; Linux fixture unit/race/vet passed in build-repaired. Change strict and git diff --check passed locally; full-library historical issues must be reported separately, never silently fixed.

## Negative evidence and risk

The first expanded matrix failed before its cross-tenant assertion because the new test called a fixed-email fixture twice with the same t.Name. Actual PG users_email_key23505 was preserved. Minimal repair only adds a named subtest to the second fixture setup; no assertion or production code was weakened. Raw original failure, proof, receiver witness and no-result ledger are in evidence/task72-negative. The earlier12-cell pass is not substituted for the final16-cell matrix.

Prior Budget151ms gap, expiry55P03 and Supervisor/SIGCONT/management timeouts retain unresolved causes and negative evidence. Historical deployment role ACL inventory remains unverified. Passing isolated tests does not establish production egress safety or complete all failure interleavings. Loss of the frozen report/ID cannot be compensated by rerunning Probe. Independent7.2 review remains pending.
