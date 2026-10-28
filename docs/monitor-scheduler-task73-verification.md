# Task7.3 — unknown recovery review index

## Product scope acceptance — Task7.3 complete,32/47 (2026-10-28)

The product owner explicitly accepted the conservative unknown scope in the
current direct user message. Six original frozen a1fc65a Artifact ZIPs and
44 retained negative-evidence files were rehashed unchanged. Both frozen
Push40613022394/PR40613027846 passed verify/integration/execution-security.
Task7.3 is now complete only within that accepted isolated fixture scope.
Precise canceled classification without trusted persisted denial provenance
remains unsupported and deferred. Historical IPv6 timeout20s remains unexplained,
not fixed; independent physical release and production hard-close are unchanged.
See the original message transcription in
`openspec/changes/add-monitor-scheduler-worker/evidence/task73-final-review/product-acceptance.json`.
Earlier pending-product/31-of47 passages are historical records superseded here.
Only Task7.4 is now authorized; no Task7.5, merge,Archive or deployment.


## Current final review gate (2026-10-28)

Implementation HEAD `a1fc65a0dbc9c53259d9b06a411250575f35fa6d` has passed
independent A code review and frozen Push/PR CI. See the
[final evidence index](../openspec/changes/add-monitor-scheduler-worker/evidence/task73-final-review/README.md)
for live check results, six raw Artifact hash checks and unchanged negative logs.
No tests were rerun for this documentation update.

The current implementation **does not support canceled classification for
unresolved started Runs lacking distinguishable, trustworthy persisted denial
events**. They remain unknown, even after independent physical release.
Existing unknown and HTTP outcomes are not relabeled; physical settlement
continues to be a separate fact. Future trusted event provenance needs its own
review, not an inferred Controller reason or new authority in Task7.4.

Product acceptance of that scope adjustment is still pending. Task7.3 is
unchecked,31/47; historical IPv6 timeout20s remains unexplained and unfixed.
The older canceled-capability and pending-CI passages below are retained
historical candidate records and are superseded by this current gate.
Task7.4 remains [plan only](monitor-scheduler-task74-plan.md), with zero proposed
permission delta and no implementation authorization. No production execution,
merge, Archive or deployment.

## Review correction — candidate, not accepted (2026-10-27)

The previous canceled claim below is superseded: current persisted stop_reason
cannot distinguish permit_denied from physical_stop_unconfirmed. The minimal
safe implementation retains unknown for every unresolved started Run. Existing
unknown/canceled/completed terminal facts and independent physical release stay
unchanged; no new schema field, role or EXECUTE is introduced.

TestRecoveryNoPermitPG now requires unknown despite a real kernel refusal and
physical stop, because no refusal cause was persisted. New RecordedDenial and
StopOnly tests use the two actual approved Worker calls with egress_unavailable,
verify their identical minimal stored shape, and also require unknown. Existing
QuotaDenied/AmbiguousDenial/TargetACK cases retain their original network and
physical proof assertions; quota/no-record classification expectations are
strictly tightened. The declared full manifest grows from20/40 to22/44 cases;
that full matrix is NOT claimed run in this correction. Directed execution and
source hashes are recorded separately in verification.md.

Historical PR run40468671798 remains failed and Push40468664422 cancelled. No
CI rerun is authorized as a substitute for finding the IPv6 timeout cause.
Task7.3 is unchecked,31/47; all older32/47 and known-canceled implementation
statements below describe superseded candidates, not final acceptance.


Scope: isolated Linux/PG18 fixture only. This is implementation/test evidence,
not an independent approval. Task7.4 and later are not implemented in this batch.
The separately approved capability and original approval message are in
[the capability record](monitor-scheduler-task73-capability.md).

## Implementation and authority

- `migrations/00008_create_monitor_scheduler.sql`: only the new12-argument
  `scheduler_private.recover_started` and its exact owner/revoke/Controller grant.
  All previously approved99role/ACL/owner statements remain unchanged; no
  Migration1–7 modification. No new Worker/Supervisor/table/column/secret rights.
- `internal/scheduler/recover_started_linux.go`: bounded explicit READ COMMITTED
  transaction, redacted errors, Commit uncertainty, known complete lineage only.
  Returns no URL/configuration/input capability. Built only with
  `schedulerfixture && linux`; ordinary release has no new entry point.
- `tests/integration/scheduler_recovery_kernel_test.go` and
  `scheduler_recovery_test.go`: separate tagged
  recovery orchestration; original joint63 driver/kernel/Probe/Supervisor and
  historical assertions remain unchanged.
- `tests/scheduler/recovery_fixture.py`, `run-recovery73.py`,
  `build-recovery73.sh`: separate legal fixture binaries, exact20-test manifest
  in each mode; first failure stops, no skips/retries or longer test timeout.
  The base network harness only extracts its unchanged prepare behavior into
  an overridable method to allocate a fresh namespace for the next Run.
- `.github/workflows/go-ci.yml`: retains every existing job/test and adds the
 40-case normal/race recovery matrix and its artifact directory.

Lock order is Workspace -> Project -> Monitor -> Activation -> Schedule -> Run
-> existing global/W/P/M budgets -> sorted origin/IP budgets -> Reservation ->
control. No late User/Session/Membership lock. All waits precede a fresh SPI
snapshot and one observed DB clock, bounded by durable time and2s. Current Owner
existence is a fresh planning check, not a locked execution authorization;
future Claim/Start must acquire their original full Owner-first locks again.

`started -> unknown` never creates a result, refunds resources, renews a Lease,
returns an input pipe or changes a Run identity. Existing unknown is immutable.
A prior completed result wins the terminal race. Known no-permit denial can be
classified canceled only on its FIRST transition after real matching committed
Supervisor release, consumed input and immutable absence of any target permit.
A durable commit_unknown/worker_lost/lease_expired reason cannot become a
known denial. Quota/authorization/stop control reasons remain separate from
HTTP failures. Target Commit ACK uncertainty is unknown even when no GET occurred.

Only trusted physical release permits arming one different future Run ID. The
compare against old next_run_id makes rollback/retry/lost ACK safe: no second UUID
or shifted due. Due is max(DB gate_now+interval,last_started+interval,persistent
not_before), no backlog replay. Current v/g/epoch, Owner, consent, authorization,
stop/readiness/disk gates must match. This planning does not extend authorization;
if it expires before the due time, Claim/Start refuse. A completed result does
not use this recovery scheduling path. No general scheduler loop or7.4+ work.

## Requirement / Scenario / Test / Evidence

All listed tests run against independent guarded PG18 and the accepted real
Linux execution/termination chain, in normal and Race modes.

| Requirement / Scenario | Tests (each normal/race) | Raw evidence |
| --- | --- | --- |
| monitor-runs / Unknown delivery; process dies after GET | TestRecoveryUnknownPG, TestRecoveryEmergencyPG | Original Header-only HTTPS GET/SNI, Run unknown, TLS revoke, real PIDFD/reap/retire, signed proof and separate Supervisor settlement |
| Unknown / Start acknowledgement lost | TestRecoveryStartACKPG, TestRecoveryInputACKPG | Actual Commit/discarded ACK, no reconstructed input, no target, unknown/quarantine without receipt |
| Unknown / Target ACK uncertainty vs known no-send | TestRecoveryTargetACKPG, TestRecoveryNoPermitPG, TestRecoveryAmbiguousDenialPG, TestRecoveryQuotaDeniedPG | Actual target reservation/unknown ACK; full connect/receiver audit; no-target denial; true physical proofs; NULL HTTP fields, fixed independent control reasons |
| Unknown / Database partition and Commit ambiguity | TestRecoveryPartitionPG, TestRecoveryCommitACKPG | Real paused PG server;2s fail closed; real Rollback and Commit/discarded ACK for classification AND future identity; immutable reconciliation, no GET retry |
| Unknown / Unproved physical stop | TestRecoveryUnconfirmedPG, TestRecoveryZombiePG | Supervisor SIGKILL/freeze; real45s Lease, still-live process snapshots; no receipt, all physical slots held even after natural network cutoff |
| Result commits / Late results; terminal race | TestRecoveryResultFirstPG, TestRecoveryRecoveryFirstPG | Independent real SQL connections behind parent barrier, pg_locks/blocking_pids/SQLSTATE/commit timeline; exactly one terminal fact |
| Immutable lineage / Scope and role boundaries | TestRecoveryValidationPG | Mixed real/invalid scope, v/g/epoch/fence/ns/Supervisor mismatch; Worker denied42501; exact Controller-only ACL |
| Unknown / Current authority after Pause/PATCH/expiry | TestRecoveryPausePG, TestRecoveryPatchPG, TestRecoveryOwnerPG, TestRecoveryExpiryPG | Actual Owner commands/status change; real60s authorization expiry; old unknown remains audit, no future authorization resurrected |
| Unknown / Later normal cycle | TestRecoveryNextCyclePG | Real DB interval>=60s, two Worker logins, different Run/fence/ns, one GET per Run and two full independent physical settlements; old unknown unchanged |
| Privacy / Sensitive sentinels never persist | All above plus TestRecoveryRejectsInvalidInputBeforeDatabase | Minimal Run/response allowlist, NULL result for unknown/canceled; original private input/redaction/connect assertions preserved |

The second cycle deliberately leaves its new attempt started with confirmed
physical release after observing the real successful HTTP response. This test
validates recovery/new execution, not general result-driven scheduling; it does
not turn the old unknown into completed. Ordinary fixed-ID result commits remain
Task7.2. No3-cycle/full scheduler acceptance is claimed here.

## Evidence provenance and retained negatives

Development commands, source inventories, binary/log hashes and raw records are
under `/private/tmp/devpilot-result73/`. Early runs are dirty development snapshots,
not one frozen implementation SHA; do not relabel them as final CI evidence.
The final CI must execute the full40-case manifest plus every historical group
at its actual clean checkout. Its Push/PR run IDs, artifact SHA256 and read-only
raw-proof audit are supplied in the final external review packet.

Retain the initial unused-import build failure, wrong harness selection (exit2,
no network) and prefix-path FileNotFoundError. In the latter a real GET occurred;
Run was unknown, global in_use/active_count1, no physical confirmation, no receipt,
no HTTP result and no returned slot. The failed harness source was reconstructed
and SHA256 matched to its ORIGINAL command receipt; it is not presented as a
contemporaneous copy. The fix restores original validator filenames and archives
each cycle separately; no verifier or physical proof assertion changed.

All earlier PID1/tracer, NSCD, permit-missing, builder, Budget/55P03/management
and SIGCONT negative evidence/uncertain roots remain. Full-library strict still
has the original four historical spec/seven length warnings; do not edit them.

## Operations / conservative recovery

1. Stop new Starts through the existing approved stop process. Retain full Run
   lineage and both namespace/Supervisor incarnations; do not discover by bare ID.
2. An authenticated Controller can abandon unresolved work as unknown; this
   alone proves no physical stop and cannot return any capacity.
3. Use the existing independent Supervisor stop/reap/retire and committed signed
   receipt verifier. Only its restricted accept_termination can return slots.
4. Repeat recover_started with the same known identity after actual trusted
   release. Retry ONLY this bounded DB control operation after an unknown Commit;
   never recreate a pipe, GET or old target permission. Future scheduling is a
   new Run and requires the normal Claim/Start gates at its real due.
5. If Supervisor/trace/PIDFD/retirement/receipt/DB confirmation is missing, keep
   quarantine. No Worker assertion, Lease expiry, container deletion, administrator
   UPDATE or manual capacity adjustment substitutes for proof. The no-input and
   lost-Supervisor fault cases intentionally remain blocked; this batch does not
   invent another physical-proof path. Use the existing stop/recovery runbook and
   independent review before any new proof mechanism or permission change.

Production remains hard closed. No merge, Archive, deployment or Task7.4.
