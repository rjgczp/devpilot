> Current update (2026-10-23 fourth review): supplemental Budget23×2 and
> historical Snapshot4×2 now passed actual guardedPG18, with runtime byte
> equivalence to the successful46 matrix. Only orchestration/docs changed.
> The following original stopped states are historical, retained verbatim.
> Task6.3 remains unchecked pending frozen-commit CI and independent review;
> see verification.md and evidence/joint63-orchestration/ for current receipts.

# Task6.3 historical failures and subsequent acceptance evidence

Reviewed baseline and current Git HEAD:
`856cbbd45aa19297e4694f7b90b2b44261049b42`.
Push39885375219 and PR39885379575 were completed/success, with verify,
integration and execution-security all success, before any task6.3 development.
All current6.3 source changes are an **uncommitted review draft**. They were not
pushed or made an accepted release. Task6.3 remains unchecked; progress28/47.

## Attempted integration and exact observed limits

The fixture-only command reuses the original private engine without changing
executor.go, dns.go, policy.go, TLS/HTTP handling, single-process admission or
budgets. A separately selected TCP-only process seal retains the offline Seal
entry's deny-all mode. The original verified database functions, role/EXECUTE
ACLs and Migration1–8 are unchanged. The new outer adapter uses the existing
6.5 stop helper definitions, public fixture OS CA trust and no-upstream per-Run
namespaces. Start/input/target commits precede their respective pipe/rule steps;
no business connection is held during the outer management calls.

The first Linux build/vet and normal/race binary compilation succeeded, but
**full normal/race joint acceptance did not pass**. Build source078d37d... and
subsequent dirty test source hashes differ; neither is claimed as a frozen
implementation SHA. Actual full hashes/commands/exits are in command receipts.
No old CI result is attributed to the uncommitted source.

| Evidence directory under /private/tmp | Command receipt | Actual outcome |
| --- | --- | --- |
| devpilot-joint63-https-smoke | devpilot-joint63-development/https-smoke.json | exit1, before Probe input: Docker Mounts excludes tmpfs; assertion mistakenly expected four Mounts instead of two read-only binds plus two HostConfig.Tmpfs entries |
| devpilot-joint63-https-mount-corrected | devpilot-joint63-development/https-mount-corrected.json | exit1, original3s management wait expired after complete A/AAAA, zero target TCP/GET; no target reservation, receipt or release |
| devpilot-joint63-https-tracer-corrected | devpilot-joint63-development/https-tracer-corrected.json | exit1, actual original-kernel HTTPS GET/200 in memory, then independent process termination/cleanup blocked; no signed receipt or release |

The two read-only bind mounts are the fixture Go binary and a public fixture CA
in the system trust directory, not a config/DB/secret/management volume.
Credential/environment/cgroup/FD checks returned UID/GID65534, caps0,
no_new_privs, all-thread seccomp,128MiB/CPU1/PID128/FD256, read-only root and
TCP-only socket creation. These observations are **partial evidence**, not a
substitute for the full acceptance or a production permission.

## First new timeout: anonymous-pipe EOF and tracer FD inheritance

An unprivileged no-network diagnostic showed that strace retained the inherited
pipe writer after its tracee closed that writer. The read side received the
frame but not EOF. The existing target protocol intentionally requires exact
frame plus EOF; that requirement was not removed or relaxed. Diagnostic output
has `eof_available_after_tracee_closed=false` and the tracer's retained pipe FD.
This proves the instrumentation defect class; the failed run did not capture
its own blocked read syscall, so its exact wait stack is not asserted as a
separately captured trace. Original3s/2s/35s/45s budgets were not extended.

A bounded daemonized-strace capability check succeeded with no additional
capabilities and python retaining PID1. Moving that tracer before pipe creation
allowed the real DNS → PG target commit → two-rule confirmation → literal TLS
connection → GET flow, but introduced the separate physical-stop defect below.
The successful capability check did not prove that this placement could stop.

## Blocking physical-stop defect: tracing the stop authority itself

The new tracer placement traced PID1 as well as the Probe. The existing stop
helper enumerates every same-Run descendant and sends SIGSTOP before SIGKILL.
It therefore stopped the tracer responsible for letting PID1 progress.

Actual read-only capture after failure:

- PID1 `python3`: State `t (tracing stop)`, TracerPid10.
- PID10 `strace`: State `T (stopped)`, PPid1.
- PID11 original Go Probe: UID65534/caps0, alive/sleeping, TracerPid10.
- Saved syscall trace ends at PID1 `pidfd_send_signal(3, SIGSTOP, NULL, 0`.
- Docker top reports PID1 `ptrace_stop` and tracer `do_signal_stop`.
- Both independent Run/Gateway revocations succeeded, veth removed,
  conntrack empty. Full process/PIDFD/retirement proof did not complete.
- Ordinary cleanup's docker rm failed with “could not kill container ... did
  not receive an exit event”. The final cleanup exception masked the preceding
  bounded management confirmation failure; both levels are retained.

This is sufficient to reject this **new tracing topology**. No signature,
process-exit guess, lease expiry, pending Docker kill or container deletion was
accepted as a termination receipt. No accept_termination/reconcile release call
ran for this Run. The previous independently accepted6.5/5.4 source is unchanged.

## Database and containment evidence

Before disposable teardown, actual PG18.6 cluster
`devpilot-budget-disposable` had global in_use1/active_count1, origin/IP slots
held, Run `started`, HTTP status/success/failure/result_commit_id NULL,
physical_release_confirmed_at NULL, reservation `reserved`, receipt/release NULL.
No active Worker/Supervisor transaction or blocker remained. The original
fixture's Go failure cleanup had removed its Controller identity; a separate
conservative quarantine call was refused **before SQL execution** by the UUID
identity guard. No replacement identity/role was created. Precisely: the
reservation remained reserved/held, not a successfully committed `quarantined`
state. The uncertain physical resources were never returned.

After read-only captures, one narrowly scoped cleanup action, with the existing
lifecycle KILL capability, killed only the stopped owned tracer. It returned137
as the already-pending container termination completed. This was teardown only,
not a signed proof or acceptance. The other four exclusively labelled fixture
containers were removed only after rechecking the disposable PG18 marker and
saving the unreleased ledger. Final matching container list was empty. No
Docker restart, host PID namespace, SYS_PTRACE/SYS_ADMIN/NET_RAW increase, DB
permission change, new execution, result write or capacity release was used.

## Required review before resuming this affected acceptance

The instrumentation must have no path in which the stop authority is a tracee
of a process it stops. It must also avoid keeping single-use pipe writers alive
in the tracer. A possible direction is to trace only a separately launched,
unprivileged bootstrap/Probe domain, allocating its anonymous pipes after the
tracer forks; that is **a proposal only**, not implemented or proven here. It
must retain the credential/FD/EOF/timeout boundaries, original kernel,
independent network revoke/pidfd/reap/retirement chain and original capabilities.
No permission expansion, time extension, missing-dependency skip or guessed
receipt is an acceptable remedy.

No more joint test or feature development was started after the physical-stop
failure. Negative cases, race run, IPv6 joint execution and the requested full
regression for this draft remain **unexecuted/not accepted**. The original76,
6.4/6.5/5.4 and API audit results for the reviewed baseline remain historical to
that exact SHA. The earlier5.4 SIGCONT/management timeout remains unresolved in
verification.md; this new diagnosis does not resolve or delete that risk.

Production execution stays hard closed. PR11 stays Open. No merge, Archive,
deployment, production migration or group7 result implementation occurred.


## Authorized tracing-only repair and second failed acceptance (2026-10-23)

**Stopped again; uncommitted; task6.3 unchecked;28/47.** The product explicitly
required stopping and retaining the draft on another acceptance failure. No
further runtime tests, fixes, commits or pushes followed this failure.

Compared with the original `pending-review.diff`, the repair restores the
accepted6.5 topology: Python Supervisor PID1 is untraced; its UID65534/caps0
child tracer traces only an unprivileged bootstrap, Probe and descendants.
Anonymous envelope/permit pipes are allocated inside that tracee after tracer
creation so the tracer cannot retain their writers. The original6.5 stop/reap
helpers, pidfd exit requirements and2s/3s/35s/45s budgets are unchanged. New
bounded observations record real /proc states, pidfd signal and waitpid events;
they do not substitute for the original termination proof. See
`tracing-repair.diff` and `topology-comparison.json` in the review bundle.

The old PID1/tracer dependency is directly supported by the saved tracing-stop,
TracerPid10, stopped tracer and incomplete pidfd_send_signal syscall. The exact
kernel wait stack/syscall completion was not captured; this remains a limit.
The separate earlier5.4 SIGCONT/management timeout remains unexplained and its
original evidence/risk is retained.

### Commands and source binding

HEAD remains856cbbd45aa19297e4694f7b90b2b44261049b42, dirty=true.
Actual build and failing matrix source inventory SHA256:
`9008c5c7e580cc80f9875463479155a269bc30ac0fff70a2bf46ce61a88bd704`.
This is a tested dirty source inventory, **not a new commit or accepted SHA**.
Only subsequent documentation/evidence changed after the failure. Complete
commands, file/binary hashes, environment/image metadata and exits are retained
in `raw/devpilot-joint63-repair-development/` and matrix receipts.

- Offline Linux Go1.24.3 build/vet and selected offline unit normal/race: exit0,
  13.546s. No network acceptance is inferred from these results.
- `python3 -B tests/scheduler/run-joint63.py --tools
  /private/tmp/devpilot-joint63-repair-tools --evidence
  /private/tmp/devpilot-joint63-repair-matrix`: exit1,13.824s.
- First case `normal/allow-https` failed the unchanged strict connect audit:
  `(allow-https,4,1)`. Planned23cases ×normal/race=46; **1 failed,45 not run**.
  Stopped/crashed tracer, Supervisor loss, timeout/fault injection and TLS
  in-flight stop cases exist as unaccepted test code, not passing evidence.
- Original76+5, API zero-target and6.4/6.5/5.4 normal/race remain unchanged but
  were not rerun on this draft after the mandatory stop. Baseline green CI is
  not attributed to the draft. OpenSpec/static checks are separate below.

### Actual trace, physical proof and conservative database outcome

The real process snapshot has PID1 TracerPid0, tracer7/UID65534/caps0,
bootstrap10/TracerPid7 and GoProbe11/TracerPid7. Independent fixture records
one TCP resolver session with A/AAAA, one literal TCP443/TLS session, correct
SNI/certificate, one credential-free Header-only GET and immediate client close
after headers while the declared1MiB body is unsent. Original engine result200
exists only in private memory, not in the Run table.

Real stop records show pidfd exits for7/10/11, actual SIGSTOP/SIGKILL and waitpid
for all three, no remaining descendants, independent dual-layer network revoke,
namespace retirement and a signed persisted physical termination receipt.
Read-only OpenSSL verification of that saved Ed25519 signature returned0; proof
hashes match the immutable bundle. This single normal case does not prove the
unexecuted fault/race/in-flight TLS scenarios or complete joint acceptance.

Unlike the first failed topology, a physical receipt **was signed this time**.
The audit failed before its delivery to the Go database consumer. No receipt
was consumed by PG and no capacity was returned: final real PG18 ledger has
reservation `quarantined`, global in_use1, released_at/termination_receipt_id/
physical_release_confirmed_at NULL; Run still started, HTTP status/success/
failure/result_commit_id NULL. A physical receipt is not an HTTP result.
Container cleanup is not used as a release proof.

The four connect calls are two AF_UNIX `/var/run/nscd/socket` ENOENT attempts
from Python bootstrap PID10, plus two Go TCP connects. Receiver evidence
accounts for the latter as one resolver and one target connection. Go sockaddr
pointers are opaque under its non-dumpable seal; the independent receiver is
retained as witness. The exact Python/NSS user-space caller stack was not
captured, so an implicit startup/NSS cause is a hypothesis, not proven detail.
No NSCD exception, relaxed count, ignored syscall or CGO/audit change was made.
Any further bootstrap correction requires renewed review; none was applied.

### Evidence and remaining gate

Original negative archive SHA256 remains
`7777a1924594af454b171d89d00c781f6e4e7b874631ba9d622874eb3bcd25f5`
at `evidence/joint63-negative/`. New failure is separately preserved at
`evidence/joint63-repair-failure/`; raw /proc, strace, pidfd/signal/wait events,
firewall/routes, receiver, signature and PG ledger are in its checksummed
archive. Review directory `/private/tmp/devpilot-joint63-repair-failed-856cbbd`
also includes the focused repair diff and full draft patch. No private signing
key is included. Files remain uncommitted for independent review as requested.

Latest actual remote SHA is still856cbbd; Push39885375219 and PR39885379575 are
completed/success **for that baseline only**. PR11 remainsOpen. No new Actions
run exists for this uncommitted draft. Migration1–8, DB ACL/functions, original
HTTP/DNS/IP/TLS kernel and accepted6.4/6.5/5.4 source are unchanged. Production
execution stays hard disabled; no group7/result persistence was implemented.


## Authorized NSCD-only diagnosis and repair — stopped on ninth case (2026-10-23)

Status remains **28/47;6.3 unchecked**. User authorized only diagnosis/minimal
repair of the two NSCD connects and a from-the-first-case complete rerun, with
**any failure stopping**. No further functional edits or runtime tests followed
the ninth-case failure. All code remains uncommitted/unpushed on reviewed HEAD
`856cbbd45aa19297e4694f7b90b2b44261049b42`.

### Original calls and deterministic attribution

The preserved original `run-0-probe.trace` lines72/74 show PID/TID10, the Python
relay (PPid7, TracerPid7; its Go child11 is created later), making exactly two:
`connect(3,{sa_family=AF_UNIX,sun_path="/var/run/nscd/socket"},110)=-1 ENOENT`.
The original trace has no native/Python call stack; that historical capture
limit remains. Its exact bytes and /proc snapshot are in
`original-nscd-calls.json` and the unchanged previous negative evidence archive.

New deterministic controls use the same immutable fixture image
`sha256:60a78b61cd1378bdb343af6bf6321f383a33f64737c54852a2d462cdfc1df15c`,
Python3.13.5, glibc2.41, UID65534/caps0/no_new_privs, network none, read-only
root, clean LANG/LC_ALL/TZ environment with no HOME. Actual strace captures all
network syscalls and operation markers. Results:

| Real Linux control | Observed connections |
| --- | --- |
| Original default interpreter startup plus actual relay imports | Two AF_UNIX NSCD ENOENT calls, before the first relay-body marker |
| Same interpreter/imports with `-S` | Zero socket and zero connect syscalls |
| `-S`, then explicit `site.main()` with an observing pwd wrapper | Two calls strictly between NSS_BEGIN/NSS_END; actual Python stack: site.main → addusersitepackages → getusersitepackages → getuserbase → _getuserbase/joinuser → posixpath.expanduser → pwd.getpwuid(65534) |
| `-S`, then direct ctypes call to the runtime libc.getpwuid | Two calls strictly between LIBC_GETPWUID_BEGIN/END, both AF_UNIX NSCD ENOENT |

This identifies the unnecessary Python site user-base/passwd query, with
underlying glibc NSS behavior, rather than guessing from a socket path. Original
native C frames and which internal glibc helper made each attempt were not
captured; no claim about those unobserved frames is made. The wrappers are only
in the separate diagnostic positive control, never in Probe acceptance code.
Those controls intentionally attempt absent NSCD in network-none isolation;
they are not newly allowed Probe destinations or audit exceptions.

### Minimal repair and unchanged boundaries

Only the fixture relay interpreter adds `-S`; its source asserts
`sys.flags.no_site==1` and no imported site module. It needs only the standard
library and no user site packages. No arbitrary HOME, custom resolver, DNS stub,
TLS bypass, libc interception or relaxed audit is used. New
`check-nscd-source.py` checks the actual launch and actual relay import list,
runs all four real controls and requires zero socket/connect for the repaired
startup. The matrix invokes it before creating any target fixture. It is not a
Mock of PG/Linux/termination.

The accepted repair topology remains: PID1 is untraced; its caps0 child tracer
controls only bootstrap/Probe descendants. Single-use pipes are created after
tracer fork. Original connect audit, PIDFD/reap/receipt assertions and resource
limits are unchanged. Migration1–8, database functions/roles/EXECUTE/ACL, Go
HTTP/DNS/IP/literal dial/TLS/SNI kernel and old network audit whitelist are byte-checked against the
reviewed baseline, together with the protected6.4/6.5/5.4 files listed in
unchanged-boundaries.json. The shared network_fixture.py retains its earlier
unaccepted6.3 adapter changes, but is unchanged by this NSCD-only repair. No group7 or
HTTP result persistence was introduced. The scoped repair diff is
`nscd-only.diff`; the full existing6.3 draft remains separately reviewable.

### Actual rerun, including new blocking failure

All build and matrix receipts bind the same dirty source inventory SHA256
`e7ef2bb27d920355b3124834900d29f63f0795a6a063412b7b31724d1329c3ae`, HEAD856cbbd.
This is not a commit SHA or a claim of frozen release acceptance. Linux Go1.24.3
build/vet and the selected offline unit normal/race commands exit0 (11.537s).
The four-control NSCD source regression exits0. Exact commands, tool/image/file
hashes and exits are in raw receipts.

`python3 -B tests/scheduler/run-joint63.py --tools
/private/tmp/devpilot-joint63-nscd-tools --evidence
/private/tmp/devpilot-joint63-nscd-matrix` exits1 after124.328s. Matrix order is
unchanged, from its first case: **8 passed,1 failed,37 not run**, zero race cases.
Full PG18 integration/integration-race, original76+5, API zero-target and separate
6.4/6.5/5.4 normal/race suites were not started after the required stop. Old CI
is not substituted for these missing reruns.

Passing real PG18/Linux normal cases: allow-https, tls-stop, tracer-stopped,
tracer-crashed, supervisor-killed, stop-ack-lost, journal-collision, allow-http.
The four confirmable stop cases have complete trace/proof/signature and atomic
PG capacity return; the HTTPS trace is exactly two connects (one real TCP DNS
session with A/AAAA and one literal target), correct TLS/SNI/cert, Header-only
GET and no credentials. The in-flight TLS stop case has actual receiver closure
and independent network/pidfd/reap/retirement proof. HTTP result columns remain
NULL; safe physical return does not complete the Run or manufacture a result.
The four deliberately unconfirmed fault cases have no signed receipt, PG
reservation quarantined, global in_use1 and no release/HTTP result. These eight
cases are partial new evidence, not complete6.3 or schemeA acceptance.

The ninth `normal/permit-missing` fails in host test log redaction:
`joint_fixture.py:82`, `base64.b64decode(v)` raises TypeError on NoneType.
Source inspection shows the denied `jointHandle` returns nil []byte, sent by
`broker.exchange("reply",map[string][]byte{"frame":reply})`; JSON null becomes
None. The redactor blindly assumes every launch/replay/reply value is a base64
string. The failing raw reply line was not persisted because redaction throws
before log append; the exact error and this source path are retained, not a
claim of a separately captured raw payload or a completed denial handshake.
No redactor fix, reinterpretation of null, audit relaxation or retry was applied.

At failure, the independent receiver witnesses only resolver TCP53 with A/AAAA,
zero target80/443 accepts and zero GET. No final child strace was retrieved, so
this failed case is **not** claimed as a complete zero-target-syscall proof.
No installed target permit/rules, receipt or capacity release was recorded. Actual PG18
ledger: Run started; reservation **reserved/held**, global in_use1, release/
receipt/physical confirmation/HTTP fields NULL. The failure happened before the
explicit quarantine step; it must not be described as committed quarantined.
Cleanup is teardown only, never a termination receipt or DB return. Read-only
post-failure docker inventory confirms zero leftover labeled fixture containers.

### Remaining review gate and preserved history

Current source is intentionally uncommitted; no push/new CI. PR11 staysOpen,
production execution hard closed. Existing Push39885375219 and PR39885379575
success apply only to baseline856cbbd, not this dirty draft. Do not check6.3.
Next independent decision is whether to authorize a strictly typed denied-frame
log/transport correction; it is not silently included in the NSCD repair.

Original PID1/tracer failure, the second NSCD failure, and the separate earlier
5.4 SIGCONT/management timeout (still without proven exact recovery stack) are
retained unchanged. New evidence is separate at
`evidence/joint63-nscd-failure/`; its archive has actual commands, original-call
excerpt, deterministic stack/operation controls, all nine case logs, /proc/
trace/pidfd/wait, network/receiver/signature evidence, PG final ledgers and hashes.
It contains no private signing key. No commit/push/merge/archive/deployment or
production target execution occurred.


## Authorized permit-missing fixture repair — matrix passed, supplementary validation stopped (2026-10-23)

Current status: **28/47;6.3 remains unchecked; no commit/push**. Only fixture/test
transport and acknowledgement assertions changed this round. The original HTTP
kernel, Target Permit Client/Server, DB functions/grants, Migration1–8, DNS/IP/
literal dial/TLS/SNI, complete connect audit, `-S` NSS controls and untraced PID1
supervision topology are unchanged from the preceding draft.

### Exact cause of the previous ninth-case error

The preserved traceback ends at `joint_fixture.py:82`: the log redactor attempts
`base64.b64decode(v)` on `payload.frame`, whose Python value is None. That value
is not a successful permission or a physical termination receipt. The real Go
Target Permit Server closes its pipe without writing on denial; Client treats
EOF as ErrDenied. The fixture's `jointHandle` verifies zero returned bytes and
returns nil []byte. Its `broker.exchange("reply",map[string][]byte{"frame":reply})`
JSON-encodes nil as an explicit `{"frame":null}`. Both the fixture redactor and
its Python relay incorrectly assumed a successful base64 frame for every reply.
This is a fixture transport/control-flow defect, not an invalid success message
from the unchanged security kernel.

The old failed raw reply line was not persisted because redaction threw before
append. It cannot be retrospectively claimed as a captured original wire. The
old error/source are preserved, and an expected negative diagnostic executes
the exact preserved old redactor on the reconstructed null message, reproducing
the same TypeError. New real normal and race `permit-missing` PG driver logs now
save the actual anonymous IPC bytes:
`NETWORK64 {"op":"reply","payload":{"frame":null}}`.

### Narrow repair and deterministic regression

The host fixture strictly validates the required `frame` key. Only an explicit
null dispatches the distinct `permit-denied` command; missing keys, extra fields,
wrong types, empty strings, corrupt or truncated allow frames are rejected. The
relay accepts that denial command only without a frame, closes its existing
single-use anonymous writer and writes **zero bytes**. It never substitutes an
empty string, creates a Confirmation, installs temporary rules or reopens a
writer. Success still requires a full2048-byte frame and original Go validation.
Start envelope4096 and receipt1024 remain mandatory, non-null, strictly decoded
and redacted. The Go fixture now checks separate reply_denied/reply_delivered
ACKs and, for permit-missing, zero installer calls and no target permit ID in PG.
These ACKs report IPC disposition, never a grant or a physical stop receipt.

Six deterministic transport unit tests use real anonymous pipes and execute
the actual embedded relay function. They check exact null-message retention,
zero-byte EOF, descriptor closure/single consumption, invalid success inputs,
required Start fields and secret/capability redaction. They are not a substitute
for the Linux/PG18 matrix and do not manufacture signed permission data.

### Actual same-source acceptance completed before the stop

All listed runtime receipts bind HEAD856cbbd45aa19297e4694f7b90b2b44261049b42,
dirty=true, source inventory SHA256
`c44586b33cc960267c2217567b34924d311aafa13d2d957d4b23879a0543bbcf`.
This is a frozen uncommitted source inventory, not a new implementation commit.
No runtime code changed while these tests ran. Subsequent edits only document
the stop/evidence. Actual commands, exits, file/binary/image hashes and raw logs
are in the review bundle; current GitHub CI is not inferred from local results.

| Acceptance actually run | Result |
| --- | --- |
| Fixture build/vet + selected Go normal/race, six transport regressions, NSCD controls | exit0 |
| Complete unchanged joint matrix, from case1 | 46/46 exit0, normal23/race23,618.543s |
| Original6.4 unit/race, dual-stack adversarial and PG normal/race | all five groups exit0,254.064s |
| Original6.5 Linux physical stop and PG normal/race | all three groups exit0,85.303s |
| Original5.4 physical receipt release normal/race | both exit0,110.927s |
| Complete Go default/fixture unit/race, vet/gofmt and original bounded fuzz | exit0,61.738s |
| Original network/execution/CI classifier unit suites | exit0 |
| Exact unchanged147-test PG18 manifest, seven groups, normal |147/147 exit0,181.326s |
| Same manifest integration-race |147/147 exit0,232.062s |
| Original76+5 isolated kernel scenarios |81/81 expected outcomes,exit0; the four deliberate bypass controls retain audit1/child0 |
| Original config/Activation/authorization/status API audit and CGO contract |exit0; real child0/audit0,approved_attempts25/blocked_attempts0; positive control retains child0/audit1 |

In normal/race permit-missing, full trace shows exactly one resolver connect and
zero target connect syscalls; independent receiver has zero target accepts/GET.
No target budget permit or temporary target rules are installed. Probe returns
result=null (not HTTP failure). Real network revocation, pidfd/reap/retirement,
pinned-key signed proof and single PG consumption return capacity, while every
Run HTTP/result column remains NULL. Successful HTTPS cases retain real DNS,
literal dial, certificate/SNI/Header-only GET and complete independent stop/
release. Unconfirmed faults retain quarantine/global1 without signatures or
release. See joint46-summary.json and raw per-case locks/ledger/process evidence.

### New supplementary orchestration failure and mandatory stop

After the completed suites, a temporary outer Budget coverage planner failed
its first inventory assertion: it wrongly required joint-pg.test to contain all
four specialized test entry points, including TestBudgetPhysicalStopPG. That
function has the independent `terminationfixture` build tag. The joint binary
correctly uses `networkfixture,releasefixture,jointfixture` without that tag;
physical-stop-pg.test is separately built with terminationfixture and has already
passed normal/race in the6.5 suite above. Static build-info and symbol inspection
confirm the mismatch. This is my added planner's wrong inventory assumption,
not a failing executed DB/network case or a reason to add tags/skip assertions.

Actual command `python3 -B /private/tmp/devpilot-joint63-denial-budget.py` exited1
at line13 `assert set(special)<=set(names)` (0.157574209s). It did not start any
supplementary Budget test body or create a PG container. The successful listing
stdout was not saved before the assertion; do not pretend the later static
symbol list is its original runtime output. This command was not wrapped by
record.py: its original functions tool chunk4f2a23 stderr/exit/elapsed are copied
verbatim with that provenance, and the exact temporary script is preserved.
No follow-up fix, retry, inventory exception or further runtime validation ran.

Consequently the remaining23 pure Budget cases per mode and the four historical
Schema6/7 Snapshot tests normal/race have **not** been completed in this round.
The latter include the live PG/network Snapshot case. No old CI/archive is used
as a substitute, and 46/46 plus147/147 do not justify a complete-history claim.
The newly added supplemental planner is outside the implementation source tree;
its failure does not invalidate the preceding actual passes, but it stops the
completion application under the user's explicit any-failure rule.

### Review state and evidence retention

Task6.3 is not checked and no completion application is made. No commit or push,
PR11 remainsOpen, production execution hard closed, no group7 implementation,
no DB permission/migration change or merge/archive/deploy. The original three
negative archives remain byte-identical, including previous PID1/tracer, NSCD,
ninth-case failure, and unresolved earlier5.4 SIGCONT risk. Original native stack
limits are retained; a later pass does not retroactively prove those causes.

Review directory `/private/tmp/devpilot-joint63-denial-review` contains the
narrow denial-only diff, full pending draft, raw/error/provenance, actual46-case
summary, all completed suite evidence, immutable hashes and binary tag proof.
Large raw traces are kept in a local checksummed archive, not pushed as new CI
Artifacts. Small permanent review records live in evidence/joint63-denial-review/.
Next independent review must decide how to correct the supplementary coverage
planner and complete remaining verification; neither is silently advanced now.
