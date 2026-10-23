# Separately authorized minimal HTTP regression repair

Original87af881: Push39740892742 success; PR39740899418 verify and execution-security
success, integration failed in race/core-04. Case31/1025 of unchanged realPG18
TestCheckpointOneAuthorizationRealHTTPStrictContract returned HTTP200 empty,
expected413. Original process exit1, clean HEAD, source4854c7c2ffd88d6567e1850d6f261107577d3130d258c4470c4f61ea03cd5643 and log hashes are preserved.
The failed process ran19.58s within its unchanged120s bound; this was not timeout,
deadlock, fixture stop or forged physical evidence. The old failing workflow was
not retried. Both old execution-security physical-stop manifests/signatures were
independently checked, but that does not erase the PR regression failure.

User explicitly authorized the proposed minimal shared production HTTP repair
and regressions after being told it exceeded the6.5-only scope. The unsafe class:
readBodyLimit immediately expires a supported transport read deadline on overflow;
net/http background-read cancellation can suppress/reject an error response before
it is committed. The exact failed CI goroutine ordering is not definitively
proven. The deterministic unit models cancellation at the immediate deadline:
all four relevant routes fail against old code and pass after the fix. It is not
a substitute for realPG18 or a claimed trace of that original CI interleaving.

The fix retains limit+1 reads, all byte/5s/2s bounds and Connection: close. It
keeps the existing bounded read deadline during overflow response construction,
then interrupts remaining reads after committing the rejection, preserving the
no-drain boundary. Actual cancellation/read errors keep their earlier interruption.
No historical assertion or real wait is edited. All Go unit/race and vet passed;
six unchanged real HTTP/PG18 tests passed normal and race in separate fresh guarded
network-none clusters (1.45s/5.91s). Source8db479acb1360ba915b09f0c093ad0396d138c16290b648cb3729e9910765243;
receipts honestly show the old HEAD and dirty=true. Runtime bytes bind to the
code commit. Final latest-HEAD CI remains a separate required check.

No Supervisor/DB function/role/EXECUTE/Migration change. Task count remains27/47;
5.4/6.3 remain pending and production closed. Large original/new raw artifacts
remain at the indexed Actions links; new final exact-HEAD results are reported
in the delivery handoff, not inferred or prefilled here.
