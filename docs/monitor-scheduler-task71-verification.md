# Task7.1 scope and review index

6.3 was separately approved at65316cc; its acceptance checkbox brings progress
to29/47. This batch implements only7.1. Final status and exact actual CI SHA live
in `openspec/changes/add-monitor-scheduler-worker/verification.md`.

| Requirement / Scenario | Implementation | Real validation |
| --- | --- | --- |
| monitor-runs: Minimal result data / Failure-Sensitive output and logs | M8 `persist_minimal_result`, fixture-only `internal/scheduler/result_linux.go` | `TestResultMinimalPG`: real HTTPS200, TLS failure, DNS blocked; fixed-code/null/numeric/secret rejection, immutable Run witness |
| stale scope/fence/incarnation rejected | Same bounded SD function, session_user binding, full composite identity, final fresh database-time check | Independent logins/connections, mixed scope/v/g/epoch/fence/ns/inc, duplicate and concurrent writers; `TestResultActualLeaseExpiryPG` actual45s wait and pg_locks barrier |
| config change while old Run finishes | Original Run identity; no new Owner lookup, current projection write or Schedule mutation | `TestResultAfterOwnerPatchPG`: real Cookie-backed Owner Store PATCH; old result stays original lineage and all post-PATCH ledger/pointers remain exact |
| preflight/permit/physical stop distinct from HTTP failure | typed control outcome; single fixed stop_reason, no result fields/state/time change | `TestResultPreflightPG`, permit-missing real zero-TCP case, Supervisor-killed real quarantine; no administrative physical proof |
| result unknown Commit must not redo network | process-local one-submission fuse and fixed uncertainty; internal nonreturned UUID only | `TestResultUnknownCommitPG`: actual PG Commit then lost ACK; second capability use and raw duplicate refuse; receiver sees one GET |
| physical release and observation are different facts | HTTP persistence requires already accepted Supervisor receipt; no resource UPDATE | original6.3/6.5/5.4 proof assertions before new observer; result-before-proof denied; exact physical/rate/cooling ledger comparison |
| unknown cannot be rewritten; control cannot fabricate HTTP outcome | started/leased single assignment only | `TestResultTerminalUnknownPG` explicit unknown validation fixture; `TestResultControlRacePG` two independent commits, exactly one fact, no mixed outcome |
| ordinary release stays dormant | all Go writer/codec files require schedulerfixture && linux; separate resultfixture test binary | ordinary Go unit/race/dependency boundary and historical API zero-target audit remain required |

Permission approval: `docs/monitor-scheduler-task71-capability.md` quotes the
original human reply to call_965917754402442daac6333bf80e0312. Only the listed
17-argument Worker EXECUTE is new. No new table/column/role/secret grant.

Actual commands:

- `RESULT71_TOOLS=/absolute/tools sh tests/scheduler/build-result71.sh` builds separate normal/race binaries and runs fixture Scheduler unit/race/vet. It retains the prior separate terminationfixture, jointfixture and Budget tag combinations.
- `python3 tests/scheduler/run-result71.py --tools /absolute/tools --evidence /absolute/new-directory` executes all22fresh PG18/Linux cells, first failure stops, no retries/skips. Original JointHarness/Probe/Supervisor networking remains unchanged.
- Existing `run-joint63.py`, `run-network64.py`, `run-stop65.py`, `run-release54.py`, `run-budget.py`, exact147integration normal/race, historical Snapshot4normal/race,81network and API zero-target remain mandatory CI steps.

7.2 is not implemented: no caller-owned result_commit_id, idempotent retry or
reconciliation, latest/current pointer write, projection algorithm or next due.
The internal UUID only satisfies the existing completed-row CHECK. Existing
execution_status naturally reads completed rows; that query is unchanged and
is not claimed as7.2 acceptance. Control outcomes remain leased/started until
future authorized recovery; a stop_reason alone does not revoke a network permit.
Worker result reporting is trusted, not cryptographic remote HTTP attestation.
Only independent physical evidence releases resources. Production is hard closed.
