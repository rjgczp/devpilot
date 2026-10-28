# Checkpoint3 review evidence: group5 independently accepted scope

Tested implementation SHA: **e05959ec7aa0e4e80981bcd59ea0d6ee4c823815**.
Last independently reviewed baseline:655d9ee9429bc3032eec0ae95246a6f3994afc5c.
The later delivery commit changes only task checkboxes, verification and evidence.
No implementation/test bytes change after this frozen SHA. Source inventory:
`615b7fd0fdbbbde73a5f6d66fd7e1f53b8a2934f7b4df3bc254bf48623ca0b96`.

Accepted tasks:5.1,5.2,5.3; Change progress23/47. Task5.4 is unchecked. There
is no independent physical termination proof, Probe, network-permit installation
or periodic network loop in this delivery. Production execution remains closed.
No PR, merge, Archive or deployment is authorized in this checkpoint.

| Evidence | Actual result |
|---|---|
| budget-normal-coverage.json / budget-race-coverage.json |14 realPG18 tests each, all exit0, no skip |
| regression-normal-coverage.json / regression-race-coverage.json |147 historical integration tests each, serial145+1+1, all exit0, no skip |
| go-unit.json / go-race.json |Full Go default build unit/race, count1, exit0 |
| linux-unit.json / linux-race.json |Full Linux fixture build unit/race, count1, network none, exit0 |
| go-vet.json / linux-vet.json / gofmt.json |Default/fixture vet and gofmt, exit0 |
| migration-hashes.json |M1–7 match reviewed bytes; all seven SHA256 logged |
| change-strict.json / diff-check.json |exit0 |
| all-strict.json |exit1: unchanged4 historical specs/7 description warnings; same output hash as CP2 |
| final-pg-state.json |Original protected PG18 schema0, no temporary Scheduler roles, other active transactions or lock waits |

See [test summary](test-summary.json), [Requirement/Scenario/Test/command/evidence
mapping](requirement-test-evidence.json), and [implementation binding](implementation-binding.json).
Each mapped receipt contains the complete actual command, exit code, elapsed time,
HEAD, clean flag, source inventory and binary/log SHA256. Its compressed original
receipt is preserved verbatim under raw-receipts; source-inventories avoids
repeating the full file list. Hash decompressed log bytes against receipt.log_sha256.
Manifest hashes the delivered artifact bytes themselves. No binaries or large
network traces are committed.

## What was proved

Real independent LOGIN connections and barriers/NOWAIT probes prove global32,
W4/P2/M1 final-slot serialization, full rollback and immediate hinted-Owner lock
release on Workspace skip. Cluster counters/tokens are not admitted by unlocked
COUNT. Nine concurrent Start transactions prove burst8; separate projects share
W burst2; a real two-second interval restores the Project token. Microsecond
arithmetic cases check boundaries and backward time for each bucket. Actual
10,000-row key capacity refuses new Claim without leaked Run/reservation/counters.
PATCH/new v/g, a new Worker connection and a larger current interval preserve
Monitor cooling. Committed Start tokens are not refunded by PATCH or target refusal.

Across real started Runs in separate Workspaces, same-origin1 and shared-IP2
concurrency apply atomically, including target-row waits and rollback after an IP
failure. One-second IP refill and a **real sixty-second** origin cooldown pass in
normal and race. Old and new HMAC generations participate together; rotation
retains the exact old tokens/not_before and at least one hour of keep_until,
refuses another early rotation and refuses while active/quarantine slots remain.
This is deadline/ledger proof, not a one-hour wall-clock soak.

A real commit with its ACK discarded returns no permit; a second request cannot
mint another. Another incarnation, wrong full scope, missing Start/input and a
Supervisor stop committed during a final lock wait are rejected. Owner/Member
status projections contain no target hash/quota/other-tenant state. Raw paths,
DNS/IP and HMAC secrets are absent from Run/reservation/catalog records; HMAC
uses canonical origin/native-IP bytes, is domain-separated, copied, redacted and
concurrently immutable. The fixture-only directory performs no DNS or TCP.

`protocol-events-{normal,race}.jsonl.gz` extracts original lock graphs and final
held ledgers with exact original log/line references. `outcomes-{normal,race}.txt`
makes SQLSTATE and real deadline outcomes readable. Budget test PG logs and
container inspect/create commands are per-test artifacts; postgres-regression
preserves the original protected server diagnostics for this round, including
expected injected timeout/deadlock cases from historical tests.

Each NEW group5 test owns an independently labeled disposable PG18 instance on
the original internal control network: no published ports, superuser elevation
of runtime identities, added container capabilities or privilege. It preserves
unreleased rows, asserts Goose Down refuses missing physical proof, captures the
ledger, then destroys only that test server and its volume. It never writes a
Supervisor termination receipt/physical-release timestamp to free slots. Existing
historical fixtures retain their reviewed administrative teardown; those results
are not physical-release evidence. A successful DB preparation or reservation
also does not prove Linux namespace, process, route or firewall behavior.

## Development failures and remaining limits

All5 nonzero developmental runs are retained under development, with original
SHA/dirty/source/binary hashes, raw PostgreSQL diagnostics and final ledgers:

1. budget-initial-0: migration helper used a relative path from container cwd;
   migration was not found before SQL. Fixed to the existing absolute repo root.
2. budget-path-fixed-2: fixture timestamp parameter lacked an explicit SQL type;
   PostgreSQL42P08 reported interval/timestamptz inference conflict. Added cast.
3. budget-target-first-1: `.test` fixture URL was rejected by existing Monitor
   configuration admission before SQL. Used reserved fixture.example.com names;
   original URL policy was preserved.
4. budget-matrix-4: new Controller registration hit23514 because the control kind
   enum lacked Controller. Added its authenticated registration kind, needed by
   the expressly approved rotation function; no role attribute/table ACL changed.
5. target-controller-0: PL/pgSQL variable k conflicted with an SQL alias,42702.
   Renamed the local JSON variable. No permission or timeout was relaxed.

These are development failures, not relabelled final passes. Final frozen runs
show no unexpected security timeout, role-check inconsistency or protocol deadlock.
Expected55P03/57014/40P01 failure injections remain in the historical regression.
Passing these interleavings is not proof that every possible interleaving is safe;
independent review is still required. The earlier Supervisor **tool-review**
timeout's internal cause remains unknown and is retained in verification.md.

Only the two specifically approved new EXECUTEs are granted. Production historical
role ACLs still have no real inventory; no production GRANT is inferred. The
catalog is bounded1000 entries/512KiB/two generations; large-catalog timing beyond
the retained2s limit fails closed, not by extending the timeout. Full1000W/24h,
retention/disk stress, target DNS/HTTP/TLS hook, independent Supervisor stop and
network-fault acceptance, new-SHA API strace audits and76 kernel scenarios remain
later work. There is no public Internet execution permission or end-to-end
Exactly Once claim. This delivery stops for independent review.
