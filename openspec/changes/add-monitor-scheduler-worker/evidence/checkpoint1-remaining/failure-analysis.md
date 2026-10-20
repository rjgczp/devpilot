# Checkpoint 1 migration interleave: stopped for permission review

## Actual result

One new test invocation ran `TestCheckpointOneOldWritersStraddleMigration` with fail-fast enabled. `activation-first` passed. `patch-first` failed; `migration-first` did not execute. The selected Go binary exited 1. No retry, permission grant, timeout extension, skip or implementation fix followed.

At 2026-10-20 07:31:37.810 UTC, backend 3034 logged `permission denied for table http_monitors`. The application Store returned its fixed internal persistence error, so the raw driver SQLSTATE was not captured by the Go assertion. The PostgreSQL log and complete prepared SQL are preserved in `pg-migration-interleave.postgres.log`. The run failed in under one second; neither database nor tool timeout was observed. The lock snapshots show the intentional migration/old-Activation wait, not a detected deadlock. They do not prove all lock interleavings safe.

## Evidence and current hypothesis

The new legacy fixture grants SELECT/UPDATE on the original business tables. Its grant is in `tests/integration/scheduler_checkpoint1_migration_concurrency_test.go`. The existing monitor configuration Store uses one data-modifying CTE statement containing both an INSERT arm and an UPDATE arm (`internal/monitor/store.go`). A missing INSERT privilege is therefore the leading explanation, rather than an established Migration8 regression. The same legacy profile was not first validated with a PATCH on schema7, so this invocation cannot establish that migration removed a working capability.

PostgreSQL18 documents required [INSERT privileges](https://www.postgresql.org/docs/18/sql-insert.html) and [data-modifying CTE behavior](https://www.postgresql.org/docs/18/queries-with.html#QUERIES-WITH-MODIFYING). These support the hypothesis; they do not replace a missing real pre-migration control.

## Required independent review

Review whether the historical API fixture should include the original monitor-creation INSERT columns. If approved, validate the same unchanged configuration SQL under the reviewed legacy LOGIN on schema7 first, then both migration orders. Do not give the legacy role Schedule/Run DML, grant functions, function-owner membership or elevated database flags. Preserve every permission-denial assertion. No such permission change or rerun was performed in this round.

Affected work remains open: old configuration writers crossing Migration8, full legacy-role capability inventory and migration/API compatibility acceptance. Checkpoint1 is not complete and Checkpoint2 must not resume. The independent persistence, HTTP authorization, real expiry and quota tests that passed remain separately recorded.

The test's own guarded cleanup succeeded. `postflight.json` records version0, no scheduler tables, no legacy test role and no active transaction. Production execution remains hard closed.

The earlier Supervisor retry's tool-approval timeout root cause also remains unproven; that risk is preserved in verification.md and its original evidence.
