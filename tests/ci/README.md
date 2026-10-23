# CI verification orchestration (no production execution)

This directory repairs PR11 CI failures at reviewed HEAD
`b7742bc247cbf8fbd59c5ff591642e5f3cc0bc30`. It does not implement scheduler6.4,
6.5, physical release or any production entry/permission. Tasks remain25/47.

## Integration process budgets

The failed run39706081660 used one `go test -timeout=120s` process for the complete
suite. At the process deadline, TestCheckpointOneRealExpiryExplicitRenewal had
only run36s of its real60s wait; earlier tests had already used about84s. A later
real45s unstarted Lease test also needs a separate process. This is a cumulative
process budget, not evidence of a stuck DB permission or a reason to weaken time.

`integration-groups.json` is an explicit147-test manifest: all142 tests previously
selected by `integration`, plus the5 already-reviewed Linux schedulerfixture
Claim/recovery tests. Five core groups contain30/30/30/30/25 tests. The real60s
authorization and45s Lease tests each own one process. Every process retains
`-timeout=120s` and `-count=1`; normal and race run serially against the guarded
PG18. The former separately repeated Activation prefix is represented exactly
once within the full manifest; no Activation requirement/test is removed.

`run_integration.py` first asks compiled Go tests to list the full build inventory.
It fails on unassigned/new, stale, duplicate or invalid test names. It preserves
all other packages formerly included by `go test -tags=integration ./...`, using
the reviewed fixture tag too. Go JSON must prove each planned top-level test ran
and passed exactly once; any actual test/subtest skip or missing package/test
success fails. Packages genuinely containing no tests are listed and distinguished
from skipped tests. The driver has no retry/skip/virtual-time/timeout-extension
path. The coverage contract tests cover inventory drift and misleading zero-test
or skipped-test success.

CI command (fresh evidence directory):

```
python3 tests/ci/run_integration.py --mode normal --evidence "$RUNNER_TEMP/integration-evidence/normal"
python3 tests/ci/run_integration.py --mode race --evidence "$RUNNER_TEMP/integration-evidence/race"
```

`plan.json`, `result.json` and each native command receipt/log preserve the exact
before-run list, actual command, exit code and Go JSON outcomes. The existing
recorder stores Git HEAD, source inventory and log hashes. Snapshot/monitor audit
and verify also publish separate receipts/artifacts. Push and PR jobs explicitly
check out the same branch HEAD instead of the synthetic PR merge SHA; frozen
checkout identity is asserted and recorded. Artifacts, not a local green suite,
are authoritative for GitHub CI success. Python bytecode is disabled to avoid
untracked __pycache__ mutating the frozen source inventory.

## Historical snapshot database isolation

The old workflow step was labelled schema7 but executed unrestricted Goose `up`,
which successfully migrated to8. All4 original Snapshot tests correctly refused
that schema. Their source/assertions remain unchanged, including real network,
minimum privileges, Cookie separation, pool release and statement snapshots.

The execution-security job has its own freshly-created PG18 service, independent
of the integration job's schema8 lifecycle. `snapshot-schema` imports the existing
`internal/testdb` guard, requires an empty public schema, checks exact native
Goose3.26.0 and executes only `up-to 6`. It then verifies schema6 and absence of
Scheduler objects. It never downs, drops or reuses a schema8/dirty database.
The test helper is compiled only with `integration`, under tests/, not an
application migration command. Missing PG18/Goose/Docker/Linux network permissions
still fail. No fixture role or EXECUTE grant is expanded.

All4 original TestSystemSnapshot tests (including live independent receiver and
v7/v8 configuration separation) run normal and race with fresh network evidence
directories. The existing network-none execution harness independently reruns
all76 kernel cases and5 Target Permit refusal cases. Its DNS/IP/TLS/GET policy,
privilege drop, resources, routes and firewall checks are unchanged. No new
network namespace/firewall/Supervisor implementation is added here.

The integration job retains its separate real Monitor/Activation/authorization/
status zero-target audit, exact two-destination whitelist, CGO source check and
NSCD counterexamples. No scheduler target exemption is added.

## Review result binding

Repair-before logs and test inventory live under the change's
`evidence/ci-repair-before/`. Final results must be read from the Push and PR Go CI
runs for the exact delivered HEAD. `CI_EXPECTED_HEAD`/checkout receipts and the
recorded source inventory identify the frozen implementation. The verification
record never treats the old run, a local plan-only check or planned job as a pass.
PR11 stays Open. Production execution remains hard closed.
