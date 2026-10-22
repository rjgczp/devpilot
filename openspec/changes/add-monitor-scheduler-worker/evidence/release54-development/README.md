# Task5.4 development evidence (not frozen-HEAD CI)

Baseline3489a41; receipts preserve actual dirty=true and source inventories,
commands, exits and log/binary hashes. Runtime code passed the enhanced real
Linux/PG18 normal/race acceptance at source3829b59ae0e4bb81dc9f03fa83cfac42f6b420db1c4a92dd2462303bfca61116.
The later implementation-binding.json compares exact runtime bytes with its
commit. This does not replace actual delivery-HEAD Push/PR CI.

Failures are retained: initial local builder embedded/src while the runner
mounted the real repository path (Goose stopped before migrations); subsequent
live45s Lease/zombie test hit the unchanged3s management confirmation timeout
on SIGCONT-only recovery, no PIDFD/retirement receipt and no release. Global32
and target slots remained held/quarantined. No failure was silently retried.
The first isolated hypothesis (SIGCONT did not resume PID1) was contradicted;
its diagnostic assertion failure is retained. A separate caps0/network-none
single-process select experiment reproduced a delayed timeout across SIGSTOP,
and an explicit control message woke it immediately. The original failing
Supervisor has no post-CONT syscall/wait trace, so its precise wait is still
not definitively proven. New5.4 recovery explicitly uses the already-approved
scoped stop protocol, preserving3s confirmation/2s pidfd/35s permit/45s Lease.
Successful normal/race does not erase this uncertainty.

Large raw traces/rules/routes/receiver proof bundles remain in exact-HEAD
execution-security Actions Artifacts. These compressed original development
logs are diagnostics, not production permission or task6.3 acceptance.
