# One bounded Supervisor database test retry

This evidence is from exactly one execution of `TestSchedulerSupervisorOwnsOneInputCapability`, at implementation SHA `3774a75eb7d3e733b174530d71816ed319d9f084`, with a clean source tree. No other tests were executed. Result: exit 0; Go reports 0.16s for the selected case.

- `pg-input-once.json`: exact Docker/test command, SHA, source inventory, binary hash, exit and timing.
- `pg-input-once.log`: actual selected Go test and native Goose setup/cleanup output.
- `manifest.json`: original security configuration limits and outer bounded-run result.
- `preflight.json` / `postflight.json`: fixture identity and cleanup.
- `pg-locks.jsonl`: pg_locks / pg_blocking_pids / transaction samples.
- `postgres-supervisor.log`: PostgreSQL server logs for this interval, including real Supervisor LOGIN permission/replay denials. This is a DB-only test; there is no Linux Supervisor process or physical-termination proof.
- `analysis.json`: prior tool-approval timeout location, current result, sampling limitations and unresolved concurrency risks.
- `retry-once.py`: exact single-use observer/wrapper; exclusive sentinel prevents accidentally running the same authorization twice.
- `checksums.json`: SHA256 inventory of the raw evidence and wrapper.

Prior attempts failed before process creation at automatic permission review. They were not observed database timeouts. Reviewer latency/unavailability is a possible explanation, not a proven service root cause. A successful run does not rule out races or deadlocks. The six samples did not capture the short Supervisor session, so they cannot prove absence of lock waits.

Runtime statement/transaction limits remain 2s and lock timeout remains 250ms; Goose CLI/context remain 30s/40s. The new 90s test cap tightens the standard Go test binary timeout, with an outer 95s emergency stop for this test container only. No timeout was reached or increased. No privilege, permission check, skip rule or server setting was changed. The read-only observer uses the already-existing fixture administrator.

The test's own cleanup returned Goose to version 0, removed scheduler tables and temporary runtime roles, and left no active transaction. Production execution stays hard closed. Progress remains 4/47. No PR, merge, deployment or later checkpoint was initiated.
