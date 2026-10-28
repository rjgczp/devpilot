# Task6.5 development diagnostics and acceptance

Only task6.5 was authorized after independent review of33fb5ed. The runtime code
commit is identified in implementation-binding.json. Only6.5 is newly checked;
5.4/6.3 remain pending, production hard closed, no new DB functions/EXECUTE/ACL.

Original command receipts/logs are compressed with deterministic gzip, retaining
actual HEAD/dirty/source/binary/log identities. Initial crypto unit failures were
a synthetic nested-map canonicalization error; canonicalizing the nested map
fixed the test, without weakening the verifier. The first extended physical test
had successfully cut the stream and reaped pidfds, then Docker archive failed to
retrieve /tmp/probe.trace. Those failures remain nonzero. Later bounded private
PID1 IPC successfully read the actual dead-child tmpfs trace; no permission or
stop assertion was relaxed. No timeout was extended or skip/retry added.

Final synchronized development acceptance: both6.4 and6.5 complete, every recorded
command exit0, common source inventory be7aa9c0177fc7df80ba2d5fd24934a177c6dc222d4b44d128e61f05495165e8.
These receipts honestly retain the old HEAD and dirty=true. Runtime file hashes
bind the tested bytes to the code commit; only latest delivered-HEAD Actions can
establish final CI. Large raw nft/route/PID/strace/receiver/PG traces are uploaded
by execution-security under stop65-evidence and network64-evidence, with manifests.

The6.5 cases exercise live TLS IPv4/IPv6 cut, direct/orphan children, both rules
lost, Supervisor loss (deliberately NO receipt), node/namespace replay and forged/
repeated/foreign-key receipts. DedicatedPG18 normal/race verify pinned signatures
but retain reserved capacity and NULL DB termination receipts/release times.
Cleanup does not count as proof. See tests/scheduler/stop65.md for limits and ops.
