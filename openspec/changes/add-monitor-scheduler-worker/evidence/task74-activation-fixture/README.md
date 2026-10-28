# Separately approved historical Activation fixture repair

Parent: `99497c3b27e2882ff3763e42ffffe425785270ef`. Human approval is in
`approval.json`; this is not an expansion of Task7.4 or a new permission.

Original Push40656977308 integration failed after120s at
`TestActivationExpiryAfterEveryProtectiveWait/updatefalse-activation`, waiting
on an unbounded PID channel. The original stack shows no live writer goroutine;
its precise early-return trigger is unknown. An expired precondition read or an
early BeginTx/PID error can structurally leave that channel empty. We do not
claim the first trigger was proven or that a later pass disproves a race.

The same SHA PR40656985730 integration passed151 normal and151race; that does
not replace the failed Push. Both security jobs were still in progress at the
saved observation. Four available Artifact ZIPs were downloaded and rehashed
against the API digest; links, SHA256 and exact bytes are retained in the index.

The minimal fix reads the strong ETag before the real150ms expiry begins;
ActivationUpdate still performs original live reauthorization. The PID barrier
now observes the actual writer result and original6s context. Every original
permission assertion and lock stage, real expiry wait, and120s process limit
remains. No production code, database grants, timeout or network policy changes.

The exact approved diff was applied; fresh network-none protected disposable
PG18 executed the one historical test with16 subcases, normal andrace once
each, exit0. The test/SQL/compiler/PG logs and source/binary hashes are immutable
in the original archive referenced by `archive.json`. JSON receipts retain their
actual dirty parent rather than claiming the future clean SHA was tested.
The final frozen Push/PR CI must independently execute the complete workflow.

Historical IPv6 route20s, SIGCONT/control approval uncertainty and earlier
failures remain unresolved evidence. No rerun-until-green, skip, expanded
allowlist, Task7.5, production execution, merge, Archive or deployment.
