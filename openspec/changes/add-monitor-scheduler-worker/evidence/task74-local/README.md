# Task7.4 local acceptance — development source, not frozen CI

Baseline/dirty parent: a1fc65a0dbc9c53259d9b06a411250575f35fa6d.
All implementation changes are limited to the existing execution_status body;
API route/Store/DTO reused, no new function/ACL/role/secret capability.
7.3 product scope accepted32/47;7.4 technical implementation now33/47,
independent review and actual frozen Push/PR CI still pending.

- pg-final/normal.{json,log},race.{json,log}:6selected real PG18/HTTP tests each,
 4new+2necessary prior regressions. Real API LOGIN, Cookie/scope/visibility/2s budget.
- physical-result/plan.json and physical-receipts/:3new tests x normal/race,
 6/6exit0, unchanged original kernel/physical termination/settlement harness.
- proof-audit.json: separate read-only raw manifest/Ed25519/full lineage/
 PIDFD/orphan/retirement/PG ledger verification,6real GETs,6signed releases;
 4completed and2unknown; unknown release never becomes HTTP result.
- real.audit.txt:55approved control connections/0unapproved targets;
 control.exit.txt: intentional bypass child0/audit1, original whitelist unchanged.
- checks/: original Go unit/race/vet/OpenAPI and orchestration receipts at their
 explicitly recorded source versions; they are not substituted for final SHA CI.
- implementation-byte-binding.json:9actual PG/network receipts match the final
 implementation/test/config bytes; full original dirty source inventories remain
 in receipts, with later documentation changes separately acknowledged.
- archive.json: complete original raw local archive path/SHA256 including
 receiver/firewall/route/strace/PIDFD/stop signatures/PG dumps/commands/negative
 evidence. Large raw traces/binaries are not repeatedly committed to Git.

Detailed Requirement/Scenario/Test mapping and accepted boundaries:
[Task7.4 verification](../../../../../docs/monitor-scheduler-task74-verification.md).

## Negative evidence and bounded repairs

- negative/initial-compile.log is explicitly a TRANSCRIPTION of the first tool
 compile diagnostics, with original failed source; it is not mislabeled as an
 original process log. New test pointer/embedded-field types fixed, no production code.
- negative/pg-first: original42P08 SQL parameter type conflict, cast only.
- negative/pg-ordering: original failure and one diagnostic-only reproduction
 (exit1 expected, no retry-to-green); actual old claimed_at was9.314ms later.
 Fixed synthetic claim ordering to one immutable timestamp anchor, no assertion change.
- negative/projection-teardown: assertions all passed, Goose Down refused
 unreleased synthetic unknown. Deleted ONLY synthetic row with no reservation
 or registered Worker; no fake release/receipt/real slot return. Real physical
 unknown transitions are separately proven by the six-case physical chain.

Each failed run stopped before subsequent tests; repaired acceptance restarted
from first selected test. All original logs/SQLSTATE/source snapshots retained.
Old IPv620s timeout remains unexplained; earlier 6.3/7.3/SIGCONT evidence kept.

CI adds4named integration tests to original147 (151normal+151race), retains all
old security cases, and adds6physical status cases. CI artifact status74-evidence
will hold new frozen raw evidence, only actual results may be called successful.
Task7.5 not implemented; no production execution, merge,Archive or deployment.
