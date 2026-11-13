# Continuous capacity — private isolated validation

Build native Linux tools with Go1.24.3 and `CGO_ENABLED=0`:

```sh
go build -tags=privateworker ./cmd/storagereporter
go build -tags=privateworker ./cmd/retentioncontroller
go build -tags=privateworker ./cmd/privateworker
```

The first two entries default to closed and perform no SQL without `--run` or
`--watchdog`. No deployment, production identity or Gate is provisioned here.
Full directed validation: `sh tests/capacity/run.sh /absolute/new/evidence-dir`.
Requires the existing Docker Linux/veth fixture, Python3 and OpenSSL3. The new
CI uses a fresh native Linux Runner; it does not create a10GiB test disk.

The operator's trusted parent supplies independent control-only containers:

| Process | Secret / inherited IPC | Authority |
| --- | --- | --- |
| storagereporter `--run --database-url-file ... --incarnation ... --bindings-file ...` | Own Reporter LOGIN; write-only pulse pipe3 | Actual bound filesystem metadata and fixed server-computed relation sizes |
| storagereporter `--watchdog --database-url-file ... --incarnation ...` | Same trusted Reporter group; read3, write4/5 | Monotonic loss invalidation and closing local admission; no physical signing key |
| retentioncontroller `--run --database-url-file ... --incarnation ...` | Existing Controller LOGIN only | Existing bounded M10 calls; no HTTP/physical-release authority |
| privateworker `--private-isolated --storage-watchdog ...` | Own Worker LOGIN, Supervisor3/4, read-only watchdog5 | Original authorized lifecycle, additional deny-only admission |

`tests/capacity/continuous.py` provides the concrete isolated wiring. Each
process is a separate non-root/caps0/read-only container. Worker cannot see
Reporter/Controller files; Probe cannot see any DB credentials. No public route,
proxy, retry, new target policy, platform or production launcher is introduced.
The existing Supervisor remains the sole physical-stop proof source.

Only safe, non-current completed history can roll under pressure beginning at96
records;100 is still a hard cap. Unknown/unreleased/unsettled rows remain held.
Schedules, Owner generation, fence, target-key cooling and current projection
pointers are retained. Each cleanup keeps the original lock order, at most8
Runs/100 row writes,2s transaction and250ms lock budget. The Controller scans
keysets in bounded batches without a connection across sleeps.

Sources: device+fsid are bound offline to actual PGDATA/WAL/default-temp and
Postgres Docker-log filesystems. PG archives, external tablespaces or unbound
topology are rejected. Reporter opens O_PATH directories and performs fstatfs;
the test also requires actual PG file reads to fail with EACCES. No directory
enumeration, PG file reads, private keys, business rows or SQL DML are granted.
R sums each scheduler table's total size once, including index/TOAST; compare
R to1GiB independently of whole-filesystem available bytes/inodes. Never add R
to filesystem usage. This is mounted-filesystem truth, not an independent disk
or lower-layer/thin-provisioning reservation guarantee.

Collect every1s following a DB nonce, publish within2s, expire at issued+5s.
Exact80% blocks. Missing, expired, failed or mismatched input fails closed;
failures never publish zero. Two fresh samples below70% restore only storage
eligibility. The independent watchdog uses Linux CLOCK_BOOTTIME pulses (3s) and
closes local admission even if SQL cannot be contacted. Result, pause/PATCH,
quarantine and trustworthy release keep their original SQL paths. No claim of
successful writes after actual ENOSPC, or global direct-SQL admission under a
simultaneous frozen DB clock and selective watcher partition, is made.

Evidence distinguishes protocol-injected boundary tests from real kernel
metadata. The scale test leaves one genuinely started, quarantined unknown
without release, then performs105 real fixture GET/Result/physical-release
cycles on another Monitor, using the actual60s interval without clock/due/cooldown
mutation. A finite3h Owner authorization and4h fixture session/registration stay
within existing limits; only the new test process timeout is3h. It verifies
count ledgers/cap, continued fence/due, retained unknown and safety writes while
storage is unavailable: the actual sampler stops after the105th GET, before
trusted release/Result, and remains stopped through pause/PATCH assertions.
An idle Worker may deny/exit then; the in-flight Worker must still settle.
No fabricated success or physical release is used.

Local first attempt correctly stayed closed before any GET: observed effective
filesystem use was about76.7%, above the70% cold-start recovery threshold. The
original failure is retained; thresholds were not changed. A native Linux CI
Runner with actual low-water samples must supply the positive scale evidence.

Exact proposed M15 and ACLs: [PERMISSIONS.md](PERMISSIONS.md), with explicit
isolated-only product approval in `isolated-test-approval.json`. M14 dependency
is unchanged, both remain outside Goose. Production application/identities/Gate
and real egress still require separate review. Migration11 absent; OpenSpec37/47,
Task8.3 pressure proof deferred. Historical55P03/lock wait/IPv6/management timeout
causes and failed-browser cleanup race remain open; no unrelated matrix rerun.

The first native CI exposed two fixture/runtime issues: maintenance selected
a never-claimed scope without ledgers, and attempted virtual cooldown rollback
was correctly rejected by the original monotonic guard. The corrected candidate
returns no_history for empty scopes; populated missing-ledger rejection remains.
The scale case now waits all105 real intervals, preserving all original guards.
Original CI artifact #14018253332 is retained with SHA256
`feef5d6e79bec46a6580e94308d0c52cea820963b1ae0d7955c79e11968e6c27`.

Subsequent failed native attempts are retained in their original CI artifacts
#14020640973, #14021516463 and #14021344084. The last captured healthy SQL state
and fresh forwarded pulses while a local Guard refused admission. A directed
Linux SIGURG test reproduced EINTR permanently burning the Guard. Poll/read
now retry only EINTR, at most four calls, without changing the3s pulse age or any
SQL/GET behavior; sustained interruption still fails closed. The fixture also
drains coalesced relay frames and allocates106 disjoint internal backend links.
These repairs require the final HEAD's own105-cycle CI; they do not establish
the root cause of every historical refusal or lock-wait failure.

Directed PG18 validation also reproduced NULL Reporter incarnation bypassing
the two equality checks. The separately approved78a2ac0 candidate uses
IS DISTINCT FROM in exactly those two predicates; NULL/wrong identities are
rejected without any ACL or budget expansion. Originalf6 and failed logs remain.

The78a2ac0 run completed31 real cycles, then the32nd Probe source exceeded the
existing198.19.64.0/23 policy. Original Artifact #14026013811, SHA256
`d3681a2ec63112ce2c9d5561906b7fac289c5bc11de116ab8ba67caab1e54d0a`,
is retained. Capacity now keeps the unknown's source pair separate and reuses
one allowed source pair only after the previous completed Run's original
trusted release and owned namespace removal. Namespace/Run/fence/Permit and
IPv6 identities remain fresh; backend links remain distinct. No network policy
or production Go was widened. Allocation tests cover all106 namespaces and
refuse reuse before settlement or while an earlier namespace remains present.
