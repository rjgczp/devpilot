# Task7.3 review round2 — blocked,31/47

User-authorized A correction and B read-only comparison; no final acceptance.
Base/remote HEAD: `f596a25553dcffb77a9e972d547d6e5a383c593d`; candidate is
**uncommitted**, not a new tested commit. Code/source inventory for all14 directed
runs: `093e195b725a25d9d55659a4a8a2e2f5cca6d4cd760f0c7825cc02a38ee71d22`.
Migration8 candidate SHA256:
`0cd9ae9654fc090422dcc2e8565bc173c9f19f26c04a1c52c2634a4c5d41a408`.

## A: classification tightened, not a new proof protocol

Existing persisted stop_reason loses the control writer's kind. Real PG18 proves
permit_denied and physical_stop_unconfirmed with egress_unavailable produce the
same stored minimal shape. Therefore merely removing the NULL alternative does
not establish a known denial. The candidate removes the whole started→canceled
exception; unresolved started remains unknown, even after a real denial and
physical stop, because no sufficiently distinguishing persisted origin exists.
Existing unknown/canceled/completed remain immutable. Independent physical release
and result commits are unchanged. No field, function, EXECUTE, role or trust added.
The planned known-canceled path remains unaccepted, not silently declared done.

14/14 directed real Linux/guarded PG18 invocations passed:7normal/7race, each
once, no skip/retry; command index contains actual exit codes and hashes.

| Test, each normal/race | Scope and actual evidence |
| --- | --- |
| TestRecoveryNoPermitPG | Actual kernel refusal, no prior stop_reason, zero target TCP/GET; real stop/receipt; unknown |
| TestRecoveryRecordedDenialPG | Approved Worker persists permit_denied/egress_unavailable; same-shape proof, unknown |
| TestRecoveryStopOnlyPG | Approved Worker persists physical_stop_unconfirmed/egress_unavailable; identical shape, unknown |
| TestRecoveryAmbiguousDenialPG | Existing commit_unknown is retained, no false canceled classification |
| TestRecoveryQuotaDeniedPG | Existing resource_limited plus permit-missing remains unknown; not a new actual quota-exhaustion claim |
| TestRecoveryTargetACKPG | Actual committed target reservation with discarded ACK; no install/GET, unknown |
| TestRecoveryUnconfirmedPG | Real fixture GET then Supervisor unavailable; no receipt, unknown/quarantine, capacity held |

Read-only raw audit verified12genuine signed physical releases,2unreleased
quarantine states and2actual Header-only HTTPS GETs (only unconfirmed cases).
All other12invocations have zero target TCP/GET. No HTTP result was fabricated.
Each run retains the original complete connect/process/firewall/receiver audit.
The declared full recovery manifest is now22tests/44mode-cases; **not run here**.
Original6.3/6.4/6.5/5.4/76+5 suites were not repeated or relabeled as new results.

## B: historical failure remains blocking

Original PR40468671798 failed normal-cross-run-envelope at the unchanged20s
Docker exec / IPv6 route setup. Push40468664422 passed that cell and its46-case
6.3 matrix but was cancelled later; it does not override the PR failure.
Both have same Runner image version/IP tooling and equivalent route parameters.
Different Runner IDs/regions and different built image IDs are recorded, not
asserted causal. Only8prior command argument differences are random veth MACs.
All46Push and18PR observed case manifests verified. Cleanup succeeded for known
registered containers, but no global post-cleanup inventory or timed-out exec/
netlink/Runner telemetry exists. Root cause is unresolved. No fixture patch,
timeout change, skipped cell, permission expansion or retry is justified.

Thus no commit/push triggers new CI while B remains unresolved. PR11 Open,
production hard closed; Task7.3 unchecked31/47, no7.4/merge/Archive/deploy.
Raw failure remains in task73-ci-blocker and original immutable CI Artifact.

## Raw evidence and one offline auditor error

Full raw evidence: `/private/tmp/devpilot-result73/review2-20261027/`.
Original successful directed test receipts/logs are unchanged. Offline read-only
proof auditor's first run failed SyntaxError from a Python backslash literal;
original source and observed tool error are in audit-negative. Its local syntax
fix passed read-only proof/hash verification, without rerunning PG/Probe tests,
changing an assertion or masking a safety failure.
