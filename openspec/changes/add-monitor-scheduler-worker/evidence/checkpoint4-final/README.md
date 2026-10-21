# Tasks6.1–6.2 independent review evidence

Frozen implementation/test SHA: **7b761cad01706ff1abfba51bf0e13f2d091fd44a**.
Reviewed starting HEAD/remote:e56c69a5832e88dfaac469071f9ada51a758dadc; clean workspace.
The later delivery commit changes only two task checkboxes, verification and
these evidence files. No implementation/test/authorization-doc bytes change.
Source inventory SHA256:215129b2ec76f6dbf6d9914d842e3533c1ebce69526851349fed614377fa0528.

Accepted:6.1/6.2; overall25/47.5.4,6.3–6.5 and groups7–10 remain unchecked.
No target DNS/TCP/TLS/HTTP, network permit hook, namespace/veth/firewall protocol,
physical Supervisor termination, result/retention or production entry is delivered.
No PR/merge/Archive/deployment; stop for independent review after branch push.

## Actual verification

| Receipt/inventory | Actual result |
|---|---|
| pg-normal/race-coverage.json |21 realPG18 tests each:7 new Start/input protocol tests plus14 related budget regressions; all exit0/no skip |
| history-normal/race-coverage.json |147 historical integration tests each, serial145+real60s authorization+real45s Lease; all exit0/no skip |
| process-normal/race.json |Real Linux input-only process tests:2 top-level tests,12 malformed/cross-lineage cases plus actual restart using consumed readers; exit0/no skip |
| snapshot-normal/race.json |3 existing no-network PG snapshot contracts each; identity, scope/privileges/release and statement consistency; exit0 |
| unit/race.json,linux-fixture-unit-vet.json,linux-fixture-race.json |Full default and Linux fixture Go unit/race,count1; vet; exit0 |
| source-guards.json |gofmt/diff-check; Migration1–8 byte-for-byte baseline and SHA256; exit0 |
| change-strict.json |exit0 |
| strict-history.json |exit1: unchanged4 historical specs/7 description warnings; byte-identical prior output hash |
| final-pg-state.json |Original protected PG18 restored to schema0; no fixture roles/other active transactions/lock waits |

[Requirement/Scenario/Test/files/commands/SHA mapping](requirement-test-evidence.json),
[test summary](test-summary.json), [implementation binding](implementation-binding.json)
and [per-task CLI records](task-completion-record.json) index the proof.
Compact receipts retain exact commands, exit codes, time, clean HEAD, source,
binary and original log hashes. Their raw-receipts/*.json.gz originals are preserved
verbatim; hash decompressed logs against receipt.log_sha256. Shared source inventory
avoids duplicating the full dictionary. Manifest hashes the delivered artifact
bytes. No test binaries or large network traces are committed.

## Start and IPC proof

New PG tests use actual independent restricted Worker/Supervisor LOGIN connections,
Owner/Member API writers, two Owners/two Workers, RC transactions and barriers.
Eleven lock-stage probes preserve Owner User→W→Membership→P→M→Activation→Schedule→
Run→Budgets→Reservation→control. pg_locks/pg_blocking_pids/DB clock and final state
are captured, including Claim/Pause and Start/Pause/PATCH commit orders. Failed
candidates roll back before any new Owner selection. Authorization expiry during
the final control wait, Owner disable, committed stop and Supervisor expiry are
revalidated after waiting. The original45s Lease is never shortened: a real30s
wait reaches timeout10+5 and Start refuses despite fresh preparation.

Real successful commits, actual commit then lost ACK and actual rollback then
lost ACK exercise both Start and independent consume_input. Unknown confirmation
returns no pipe/snapshot; the shared sender is consumed even on failed IPC/input
ACK. Reading started cannot reconstruct a snapshot. Confirmed Start retains its
old v/g/URL after a later Pause/PATCH, consistent with Start-Commit in-flight
semantics; no target request is sent. The pipe receiver consumes once and retains
full W/P/M/run/v/g/epoch/fence/namespace/Supervisor binding. Pool acquisition and
pg_stat_activity show no controller/Supervisor connection or transaction while
offline input reception/bridge runs. This is not the prohibited live-network
stall experiment; that scenario remains a later network acceptance dependency.

A4096-byte anonymous-pipe frame and separate1024-byte trusted descriptor receipt
pin exact canonical JSON, full lineage and SHA256. Oversize, truncation, trailing
frames, changed bytes, invalid/cross-run/scope/version/epoch/fence/namespace bindings
are rejected. The receipt digest binds trusted descriptors; it is not public
user authentication or an HMAC key. A malicious trusted controller fabricating
both pipes is outside this receiver claim. There is no replay file or reconnect
API; a restarted process using the original readers gets EOF. Production entry
and environment unlock remain absent.

The process-race binary instruments the test driver; its child is the same
CGO=0 input-only Probe as normal, preserving the credential-free build boundary.
The separate fixture race suite also instruments the shared receiver/codec.

## Real process boundary

process-*-create.json and final-inspect.json prove network none, read-only root,
only two read-only binary mounts, no source/API config/credential volume/socket,
128MiB memory+swap, CPU1,PID128,FD256. The trusted test parent has only three launch
caps; setpriv drops every child bounding/inherited/permitted/effective/ambient
cap to0, clears groups, sets UID/GID65534 and no_new_privs. The child has only
LANG=C/TZ=UTC despite DB/Cookie/cloud sentinels in its parent. Actual all-thread
TSYNC seccomp denies socket creation (including alternative io_uring), management,
exec and namespace changes. No resolver, dialer, HTTP client or engine is called.

Actual cgroup/rlimit/readonly state, all-thread caps/NNP/seccomp and inherited FD
checks plus real EMFILE validate the boundary; parent live /proc observations are
logged separately. Probe output contains only bounded numeric/boolean proof.
URL and credential sentinels do not appear in argv/env/ordinary diagnostics or
persistent Run data. The resource cap applies to the whole fixture process group,
stricter than the child alone; dedicated per-Run physical supervision is later work.

The bridge constructs the original private monitoring Snapshot and reuses its
original offline validation. It does not expose execution. Original executor/DNS/
TLS/HTTP/admission and audit whitelists are unchanged. The process filter only
removes permissions; it is not6.3 Target Permit or6.4 routing/firewall acceptance.

## Failures, scope and remaining risk

One developmental PG run failed23514 because its stop fixture tried the illegal
combination gate=fixture/emergency_stop=true. Raw receipt/logs are in development;
only the fixture was corrected to close gate and stop atomically. No constraint,
ACL or timeout changed. Final frozen runs all pass. Expected historical55P03/
57014/40P01 fault injections remain expected. Passing selected interleavings does
not prove absence of every concurrency defect; independent code review remains.
The earlier Supervisor tool-approval timeout's internal cause is still unproved.

[Current product-owner supplement](authorization-supplement.json) is the source
for the two Checkpoint3 EXECUTEs, distinct from Checkpoint2 grants. This round adds
no SQL function/grant, role attribute, table/column or secret permission; all8
migrations are unchanged. Historical production role ACL inventory is still absent;
no production grant is inferred.

Each new PG test has a fresh guarded disposable cluster, preserves unreleased
rows and asserts Down refusal before destroying its owned test server. It never
writes a physical termination receipt/timestamp. DB preparation/input consumption,
child exit and fixture teardown are **not5.4 physical-stop proof**. No capacity
release is added. PG issuance and standalone sandbox are separate real proofs of
the same protocol/SHA, not the later combined Supervisor/periodic runner.

Original76/executionnet,live snapshot execution and API target-strace audits are
not executed/claimed in this batch because target traffic is expressly prohibited.
The3 selected snapshot tests are recorded as a subset, not the whole live suite.
6.3 target-permit hook,6.4 network controls,6.5 independent termination and5.4
physical release remain dependencies. Group7 unknown state reconciliation/results,
group8 retention and full periodic/network acceptance remain incomplete. Unknown
Start currently returns no capability and retains resources; this batch does not
claim group7 recovery. Production execution remains hard closed.
