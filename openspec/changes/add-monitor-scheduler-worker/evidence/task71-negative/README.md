# Task7.1 local preflight negative evidence

The first matrix stopped at the first race-mode case after seven normal cases.
The raw race stacks show a test goroutine still executing deferred pgx Rollback
while its parent returned that connection to the pool, where the next subtest
acquired it. The actual receiver/physical proof and SQL result succeeded; this
is still a failed acceptance, not a pass. The fix hands connection ownership
to the goroutine and notifies only after rollback/release have completed.
No scheduler SQL, isolation, assertions or limits were weakened for this fix.

These were dirty, evolving local preflight sources, not a frozen acceptance SHA.
Keep original command receipts/source and binary hashes; they do not certify the
subsequently extended final tests. Final source-bound acceptance is recorded
separately. Earlier builder same-file cp failures occurred before PG tests.
All original6.3 negative evidence and5.4 uncertain timeout risks remain.

Files are byte-preserving deterministic gzip; manifest hashes cover original bytes.
