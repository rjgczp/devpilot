# Dormant single-execution kernel

This internal package is not imported by the API. It has no production trigger,
worker, environment switch or executable. Only controlled test code invokes the
unexported execution core. Saving/updating a monitor never starts execution.

The system snapshot reader uses independent trusted controller credentials,
one tenant-scoped statement, and a two-second database budget. It releases the
connection before returning. Snapshot fields are private and all formatting
redacts them. A scope or a user cookie is not a system credential. The read-only
role used in integration tests is a temporary fixture, not a migration or a
production worker account.

Results are in-memory observations of a specific complete scope/version. They
never update current health. UTC timestamps accompany a monotonic duration;
preflight rejection is not a failed network probe. The execution kernel still owns no result table or production entry. Migration7
now adds separate dormant activation intent through the configuration API; it does
not authorize this kernel, schedule work or persist results. Migrations1–6 remain
byte-identical. Schema8 Activation keeps the same field names/types and uses the deprecated
fixed migration pair not_reported/execution_status_required with intent-v2.
execution-status is the sole runtime authority. Schema7 historical fixtures
retain their original blocked/scheduler_unavailable contract.

## Snapshot and address policy verification

`snapshot_pg_test.go` is built only with `integration,executionsnapshot`. Run it
serially after explicitly applying existing migrations to the guarded disposable
PG18 database. Its real LOGIN role receives only the configuration columns and
parent key columns used by the single SELECT. The fixture temporarily removes
PUBLIC's default TEMP grant in that disposable database and restores its ACL;
this prevents TEMP DDL through implicit PUBLIC membership. No production role
or migration is created. Writer transaction/commit barriers prove complete
old/new statement snapshots; the reader closes Rows before any network work.

The four original IANA CSV files under `registry/` are immutable build inputs
with source URLs, retrieval dates and SHA256 in `manifest.json`. All special-use
entries are denied, including those marked globally reachable. Allocated/legacy
space is only a candidate, not proof of RIR assignment or reachability. Native
IPv6 is restricted to allocated 2000::/3; mapped, translation, tunnel and known
ISATAP forms are denied. Operator CIDR denials precede allocation checks.
Registry updates require a reviewed source/hash/vector change. Runtime never
downloads registries. Arbitrary NAT64 prefixes and transparent destination NAT
cannot be inferred from address bits: independent routing/egress isolation is
required and no production execution entry is provided here.

DNS uses one fixed resolver IP/TCP53 connection, absolute A and AAAA questions,
bounded complete wire messages and consistent CNAME chains. Both families finish
before target dialing; dangerous addresses anywhere in received sections reject
the observation. Only terminal answer records are selected, never glue. There
are no retries, fallback resolvers, system search/hosts or cached approvals.

## Execution limits and semantics

Every kernel instance shares the process admission gate. Accepted probes are
limited to32 total,4 per Workspace,2 per Project and1 per complete Monitor scope.
The gate atomically reserves global32/s burst32, Workspace4/s burst4, Project2/s
burst2 and Monitor interval cooling, with no queue or goroutine per rejection.
At most1024 total token/cooling/scope records exist. last_start remains for the
maximum3600s interval, active entries cannot expire, and version/interval changes
cannot clear cooling. Cancellation releases active slots but accepted attempts
still consume rate. These are process limits, not distributed quotas.

The admitted saved timeout(1–30s) and parent deadline bound the whole probe;
DNS2s/TCP3s/TLS3s/headers5s each take the remaining minimum. DNS admits at most
8 CNAME jumps,18 exchanges,128 RR,32 unique terminal addresses,16KiB/message and
64KiB total, with one TCP DNS connection. All observations are checked before
selecting one address deterministically (IPv4 first, then numeric order).

Only that literal IP is dialed, with no Happy Eyeballs/fallback/retry. The dial
closure captures the actual probe context independently of Transport's context,
and verifies origin/port and RemoteAddr before TLS/HTTP. Tests exercise the
origin/second-dial permit and a mismatched connection wrapper; those unit proofs
are explicitly distinct from the real DNS rebinding/connection evidence.

TLS preserves the original SNI/hostname with normal roots and TLS>=1.2; the
custom dial explicitly enforces3s and128KiB handshake input. The same raw-byte
ceiling is retained for the entire connection, so post-handshake TLS control or
padding cannot bypass it. This conservative shared wire ceiling may reject an
unusually large handshake-plus-response before the plaintext ceiling. HTTP plaintext is
capped at64KiB, each header block at32KiB and nonterminal1xx at4 (including the
standard parser's accepted LF form). There is no proxy, cookie jar, inherited
credential/header, TLS session/connection reuse, HTTP2/h2c/HTTP3/upgrade or redirect
loop. A single GET/HTTP1 RoundTrip compares a complete final200–599 status. An
expected302 can succeed without contacting Location. Body application reads are
zero; close the socket before Body.Close, never drain/decompress. Limited TLS/
parser read-ahead is counted and is not described as zero bytes on the wire.

Fixed internal failures are dns_failed, blocked_target, connect_failed,
tls_failed, timeout, canceled, response_limit, protocol_error and
unexpected_status. Invalid configuration/snapshot/admission failures return no
Result. No raw network error/URL/path/header/body/certificate is logged or
returned. A lost result/canceled GET does not prove the target never received
the request; no automatic retry/compensation follows. Old admitted snapshots
can finish with their old timeout/status/version after configuration or user
authority changes; no result is ever published as current Monitor health.
