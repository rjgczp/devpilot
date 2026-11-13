# Continuous capacity — exact inactive forward candidate

`continuous-capacity.sql.candidate` is a proposed future Migration15, outside
Goose. It depends on the unchanged PR23 Migration14 candidate, SHA256
`20a4c1b1f7ae9f968984d8c12aba80f3e52a5d85a126c96c21bd953aa9c18a0a`.
Migration1–13 stay unchanged,11 stays absent. Neither candidate is a production
migration or an authorization to open a production Gate.

New authority, all included in `permission-diff.json`:

- One NOLOGIN group `devpilot_storage_reporter`, without superuser/DDL/replication/
  bypass/RLS attributes. CONNECT only to the dedicated current database and
  USAGE of `scheduler_private`. No Worker/Controller/Supervisor membership.
- Two owner-private tables: `storage_registration`, `storage_state`, owned by
  the existing scheduler owner. No direct table/column grants to any runtime.
  Offline administrator binds one Reporter LOGIN/incarnation/generation, actual
  postmaster instance, expiry, and1–4 distinct filesystem device+fsid identities.
- Reporter group EXECUTE only `storage_begin(uuid)` and
  `storage_report(uuid,uuid,jsonb)`. Independent sampler and deny-only watchdog
  share this trusted group; the watchdog is not claimed to have narrower SQL
  authority. Test LOGIN provisioned only in the disposable DB after approval.
- `storage_admission(timestamptz)` is owner-private; the existing API definer
  owner receives boolean EXECUTE for status projection. No Worker, Controller,
  Supervisor or API LOGIN receives a new grant or table privilege.

Seven existing functions are replaced, with source hashes in the manifest:

- `run_capacity_accounting()`: still enforces the100/10000/100000 caps. Always
  rejects deleting unknown/unreleased Runs. A physically released, settled
  historical completed Run may retire while its schedule/budgets remain live;
  current attempt/result pointers remain protected. No slot is released here.
- Existing `cleanup_monitor(uuid,uuid,uuid,uuid,timestamptz,uuid,integer)`:
  reuse original parent/Run/G/W/P/M/target/Reservation/control lock order,
  physical receipt checks, max8 Runs/max100 writes. Under pressure (Monitor96,
  Workspace10000 or global100000), prune only settled completed history during
  active authorization/cooling. Keep schedule, fence, budget keys, cooldown and
  current projections. Target-key cooling still governs key deletion separately.
  Unknown stays retained; other old retirement rules remain conservative.
  An empty scope returns no_history after parent locking and a fresh Controller
  identity check, without touching budgets. Non-empty missing-ledger failures
  remain unchanged; this does not delete empty schedules or return slots.
- `claim`, `start_run`, `reserve_target`: add storage admission to original final
  disk check after existing waits. No signature/Owner/HTTP/Permit policy change.
- `private_worker_ready`: own boolean also requires storage admission.
- `execution_status`: reuse `retention_capacity` when the sample is unavailable;
  existing tenant authorization and reason priority remain unchanged.

Sampling: actual Linux `fstatfs` on pre-opened O_PATH directories, no file reads,
directory enumeration or symlink following. Match registered device+fsid.
An operator must map PGDATA/WAL/log/temp to every bound device; unbound topology
stays closed. Root/PG private directories remain0700/files0600; the independent
UID needs only search access to the mount root, not PG contents, keys or rows.
No pg_monitor, server-file, program-execution, CREATE/TEMP or OS capabilities.

R is the server-computed sum of total sizes of six scheduler tables and the two
storage tables (indexes/TOAST included once). It is compared separately with
1GiB. Each bound filesystem includes all allocations/WAL/log/temp on that
filesystem; R is never added to filesystem used space. This observes the mounted
filesystem, not an independent10GiB volume or lower-layer reservation proof.

Challenges issue every1s, complete in2s, expire at issued+5s. Missing/expired/
malformed/replayed/unbound samples close admission.80% space/inodes/relation
budget blocks; two fresh samples below70% restore storage eligibility only.
Failures never publish zero or reopen deployment/Owner authorization. The
independent monotonic watchdog invalidates SQL and closes inherited local
admission pipes on loss; it cannot manufacture physical release or clean runs.

Approval requested only for disposable PG18/Linux/no-upstream validation,
including unchanged M14, M15, a dedicated Reporter test LOGIN/group membership,
registration, private-isolated test Gate and UID65534 metadata-only read-only
mounts. No production, privileged mounts, ACL expansion or new physical disk.
Validation will include actual statfs plus injected threshold/refusal samples,
and105 actual60s fixture HTTP Run/Result/physical-release cycles. No clock, due
or cooldown is mutated. Fixture control/Reporter registrations and an existing
Owner session last4h; Owner requests a finite3h authorization through the existing
API (within its existing24h maximum). No authorization rule is changed. The
new test wall-clock timeout is3h; SQL2s/lock250ms/Lease/Permit limits stay fixed.
No Internet GETs and no Task8.3 physical-pressure acceptance are implied.

Pause/PATCH, Result, quarantine, trusted release and recovery are not gated by
this new storage helper. Existing budget/physical guards remain. No permission
is applied until the exact candidate hash and this scope are approved.

Previously approved corrected candidate SHA256: `f6eaa65dfd8a7178e7ba4473f1262e5f2710db1d8cae98bed61e6196575c1928`.
The only SQL difference from approved79c2db9 is the empty-scope early return
above. That original candidate and failed CI artifact are retained. The corrected exact hash and real-interval test scope were independently
approved on2026-11-13 and are recorded in isolated-test-approval.json.

Independently approved source-binding correction SHA256:
`78a2ac0e388ea0d4fb46e046b624315a88f07b82b58fe5784e246815197fa212`.
Disposable PG18 reproduced `storage_begin(NULL::uuid)` succeeding (00000) where
42501 was required. SQL three-valued comparison did not reject a NULL supplied
incarnation for the registered Reporter LOGIN. The sole candidate differences
are two `r.incarnation<>p_incarnation` comparisons becoming
`r.incarnation IS DISTINCT FROM p_incarnation` in storage_begin/storage_report.
No roles, GRANT, gate, threshold, lock order or timeout changes. The product owner separately approved this exact SHA on2026-11-13 for the same
isolated-only scope; approval JSON now binds78a2ac0. Production remains closed. Original
f6 bytes and the failed directed fixture are retained outside the repository.
