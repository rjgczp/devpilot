# Frozen f94da04 Push integration failure; separately authorized fixture repair

Run40259060171 job121367210842 failed historical
TestCheckpointOneStartExpiryAfterFinalWait:55P03, not requiredP0001.
Real PG waiter758 blocked by756 in scheduler_control; xact_start02:28:44.132649,
snapshot02:28:44.139091, lock timeout02:28:44.384 UTC2026-10-26.
This was not a120s process timeout and not a7.1 result failure.
Exact expiry/barrier-commit/scheduler-descheduling timings were not captured;
the initial root cause cannot be determined conclusively. Verbose lock output
was on the critical path. Removing that output from the held-lock section is a
minimal approved test-orchestration improvement, not proof that logging was the
sole cause or that shared-runner concurrency risk is eliminated.

Human separately approved reply call_5e506edea0de4ce78bd5d4912f08b166.
The patch still captures the original live pg_locks/pg_blocking_pids snapshot
while blocked, waits until the same real DB deadline, commits the same barrier,
then emits the snapshot.200ms administrative fixture expiry,250ms lock timeout,
2s transaction budget and strictP0001 assertion are exact. Public60s authorization
and45s Lease tests unchanged. No production/Start/SQL/role permissions change.

Actual guarded fresh PG18 directed normal+race both pass, including the original
strict candidate_invalid/state/token assertions and real lock snapshot. No Probe
was launched or target TCP attempted; these historical DB-only fixtures are not
physical termination proof. Original failure/raw commands/hashes retained here.
Complete frozen Push/PR rerun remains required;7.1 is still unchecked.
