# Task7.3 delivery blocked — 31/47

Implementation and remote HEAD: `f596a25553dcffb77a9e972d547d6e5a383c593d`.
Source inventory: `ddbd3475dae7dc3cd2bc62ea168f07cbdeee9685617113a92cd8c7d616b59257`.
**7.3 is incomplete; independent review is unsigned.** No7.4, merge, Archive,
deployment, production network, permission expansion or retry followed failure.

## Actual CI

| Event | Actual result | Link |
| --- | --- | --- |
| PR | failure in original6.3 fixture construction; verify/integration success | [40468671798](https://github.com/rjgczp/devpilot/actions/runs/40468671798) |
| Push | cancelled after PR failure to stop acceptance; verify/integration success | [40468664422](https://github.com/rjgczp/devpilot/actions/runs/40468664422) |

The separate earlier5b4d22b runs40467141311/40467147152 were cancelled to
strengthen actual second-tenant scope tests. Those are superseded candidates,
not this actual PR failure, and none is reported as full success.

## Exact failed case and boundary

The PR original6.3 matrix attempted18normal cases:17passed, then
`normal-cross-run-envelope` failed, exit1. The full46-case suite and later7.3
matrix did not complete. Failing construction command, started
2026-10-27T08:01:35.941382Z:

```text
docker exec -i 2ae1885b7271b9fb8e33e0c9aa8afa332b6cb288ddf22adb657647cd3593b404 ip -6 route replace fd64:53::53/128 via fd64:64:1::3 dev eth0
```

`subprocess.TimeoutExpired` after the unchanged20seconds:
`network_fixture.py checked()` -> `Instance.__init__()` inside `Harness.pg()`.
Full traceback, original command/source/binary/log receipt and fixture records
are preserved. No timeout extension, changed assertion, skip, route/firewall
weakening or test retry was applied.

This precedes **this case's** PG18 container/driver creation, Supervisor prepare
IPC and Probe launch. The receiver has only ready, zero accepts and zero GETs.
No PG driver/lock graph or Probe strace exists because those components had not
started. This is **not a passing zero-TCP denial test**: its assertions did not
run. No Run, HTTP result, signed termination receipt or DB resource settlement
was created in the failed setup. The CI job's earlier shared PG service is a
separate component and is not evidence of a DB wait in this case.

## Cause proved versus unresolved

The proven boundary is a20s subprocess/Docker-exec timeout. Existing evidence
cannot distinguish Docker control scheduling, exec startup, an executing
iproute/netlink operation, kernel state or Runner load. Remote process stacks,
underlying syscall/errno and daemon telemetry were not captured. No deterministic
root cause is claimed; it is not dismissed as transient.

The existing Instance constructor is byte-unchanged. The7.3 shared prepare
extraction is invoked after PG/driver setup, later than the failure. New
recover_started SQL/Go was not entered. That locates the failure but does not
exclude indirect build/load/concurrency effects. Local successes and the other
Runner's successful46cases do not prove absence of a concurrency problem.

## Actual evidence and provenance

All six final-SHA Artifacts were actually downloaded and checked for size and
SHA256 against GitHub metadata. The PR failed security Artifact is12719305073,
8181118bytes, SHA256:

`8d3a5acf0eaa53bec18a66fd84db872b4bf8cadce66fa55bdbed684e7468f970`

Its failed command receipt binds clean f596a25/source ddbd3475…59257. Full ZIP and
raw files are under `/private/tmp/devpilot-result73/ci-audit/final-artifacts/`.
The local repository's `evidence/task73-ci-blocker/` preserves the raw failing
case, traceback, incomplete test-plan/result, metadata and SHA manifest.
The final review bundle additionally includes the actual CI/job metadata,
read-only receipt/proof auditors and the complete code diff against approved
e767a5ad961a47cd323768aaafdd99ca83b55bbe.

Both final-SHA integration Artifacts were checked independently:147normal and
147race each, no skips/duplicates; API audit25approved/0blocked, including its
expected failing bypass calibration. These are successes, not substitutes for
the failed security suite. Push completed46joint63 and24legacy7.1 cells before
cancellation; current7.1 stopped after4/6cells. Neither event ran7.2/7.3's final
matrices. The PR stopped at17/46successful6.3 cells.

Local development44invocations cover40distinct7.3 cases and were technically
successful, but are multiple development source snapshots. They cannot replace
the missing clean final-SHA40-case CI matrix. Their raw signed physical proofs,
DB accounting and independent receiver data are separately indexed; no old
result is relabeled as current acceptance.

## Stop state and review boundary

7.3 was checked before the final CI gate; that was premature. The **local**
checklist is corrected to unchecked,31/47. Failure records/checklist corrections
are intentionally uncommitted/unpushed to avoid triggering an automatic full
rerun after the safety timeout. Remote f596a25 remains the earlier candidate
with7.3 checked; this blocked report and the local correction diff supersede
that completion claim. No production Go/SQL changed after freeze.

The original three development failures and all earlier PID1/tracer, NSCD,
permit-missing, builder, Budget/55P03 and management/SIGCONT negative evidence
and unresolved roots remain. Production stays hard closed; PR11 stays Open.
Further diagnosis or a bounded retry of this historical6.3 setup failure needs
independent review/explicit direction; no speculative fix is proposed.
