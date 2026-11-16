# Private test release preparation — NOT a release approval

Baseline: PR25 `9c497d372661f95722a7176a613579578bcdd02f`, merged as
`d85204335d101534e4595e64632dd4e36fcd59aa`. This directory adds read-only checks
and a management-network override. It does not start a process, connect to PG or
a target, create resources, read credentials/private keys, apply SQL/ACL, or open
a Gate/route. OpenSpec remains37/47;8.3 and production M14/M15 remain deferred.

## One preflight entry

```sh
cp deploy/release/release.example.json /absolute/private/release.json
# Edit references to the already prepared nonsecret management .env, private
# secret directory, existing service.json and persistent journal; create nothing
# with this checker. service.json is the existing PR25 private-isolated format.
sh scripts/private-release.sh --manifest /absolute/private/release.json
# Exit2: real private HTTPS release remains blocked, even when local checks pass.
sh scripts/private-release.sh --manifest /absolute/private/release.json --config-only
# Exit0 means STRUCTURE ONLY, never permission to deploy/execute/migrate.
sh scripts/private-release.sh --manifest /absolute/private/release.json --inspect-host
```

Offline checks reuse the existing management renderer and service config loader,
verify the exact source/forward SQL chain, certificate name/remaining24h lifetime,
secret-file metadata, and journal recovery guard. They do not validate private
key material/pair matching, CA chain trust, effective SQL roles, actual Gate,
registration/Owner authorization, filesystem canonical FSID or firewall policy.
These appear explicitly as unverified; `private_https_release_ready` is always
false in this revision. Secret content is never echoed/hashed/read; only the
PUBLIC certificate is parsed by bounded offline OpenSSL. Files0444 are allowed
only beneath their operator-owned0700 host parent; files0400/0600 must be owned
by their intended container UID. No ACL is widened. Use the existing deployment
secret instructions; no password belongs in this manifest, argv, Git or image.

`--inspect-host` is additionally read-only and requires native local Linux/Docker,
not Mac/Docker Desktop/remote DOCKER_HOST. It checks an existing WireGuard/TUN
interface/address, internal IPv6 control network, running pinned PG18 without
published ports, actual PG volume/log metadata mounts, and PGDATA/WAL/default
temp/journal device coverage. It rejects external tablespaces/symlinks. SQL
settings (temp_tablespaces, archive/replication slots, logging mode), canonical
FSID/postmaster registration, real VPN peer access and host/cloud firewall still
require separately authorized host acceptance. No network request or SQL query
is made by this checker. Failed daemon/secret payloads never enter its report.

The journal/key must persist with PG data and private backup custody. A burned
Run lacking a committed receipt/proof bundle, or existing `recovery_required`,
blocks configuration readiness. Files/flags are not signatures or DB proof;
`recovery_clearance_verified` stays false for any retained journal; even complete
bundles are only pending until the existing Supervisor verifies
and reconciles them. The checker never signs, deletes, clears or returns a slot.
Parent death without a complete trusted proof stays closed for operator recovery.

## Minimal configuration, reusing current components

| Component | Existing entry / state | Preparation still needed |
| --- | --- | --- |
| Management API + React HTTPS | `deploy/private/compose.yaml`; Secure Cookie, Origin/CSRF, auth limits already implemented | VPN IP/private DNS, trusted cert/key, invited/noninvited peer tests |
| PG18 + migrator / API identity | dedicated persistent volume; tools profile only; API gets its own credential | reviewed offline M13→M14→M15 and identity binding; never on normal startup |
| Two Workers + Supervisor + Controller | `scripts/private-service.sh`; real standalone binaries/anonymous IPC, original stop proof; Controller includes retention | five separate LOGIN files, offline incarnations/expiry/catalog, pinned public key |
| Reporter + independent watchdog | actual metadata,3s monotonic guard,5s SQL sample;1GiB relation/80% gate unchanged | device/FSID/postmaster binding, readonly metadata paths and actual content EACCES |
| Private Probe / network managers | one GET, DNS/IP/Permit/literal/TLS protections already implemented | REAL routed target/resolver lifecycle remains unimplemented below |

The management-only database network was IPv4-only. The following **render-only**
override prepares an internal dual-stack network named `devpilot-private-control`,
matching service.json; it adds no executor, public route, host port or privilege:

```sh
docker compose --profile tools --env-file deploy/private/.env \
 -f deploy/private/compose.yaml -f deploy/release/compose.management.yaml config
```

Apply this only on a NEW dedicated authorized stack. Do not replace a live
network to change IPv6. Existing DSNs must name `postgres` on that control network
and database `devpilot_private`, not test-only `pg/devpilot_registration_test`.
Use the existing private management and service startup instructions after
separate deployment approval; normal API/web startup never runs migration tools.
The runtime image is an immutable SHA256 ID. No fixture test binary is a service.

Use persistent PG18 default data/WAL/temp storage, rotated Docker stderr logs,
and a persistent0700 journal/private signing key; do not use container layers or
ephemeral `/tmp` for a deployed journal. Its device must be among sampled devices.
No independent10GiB disk or extreme pressure proof is a new release prerequisite.
Management's initial2vCPU/4GiB/40GiB allocation is not a measured combined-service
capacity guarantee: current PG cap1GiB, seven control-child caps256MiB each, and
per in-flight Probe/run/guard caps128MiB each require a host budget including
Docker/OS/headroom. Bind actual CPU/RAM/free bytes/inodes before authorization;
keep storage's existing thresholds rather than substituting a disk-size guess.

## Exact database order and pending authority

`release-lock.json` freezes filenames/SHA256/dependencies. Order is **1–10,12,13,
then candidate14, then candidate15**.11 remains absent. Neither candidate is in
Goose or the management image's migration directory; normal `migrate up` stops
at13. No file is copied, renamed, renumbered or applied by this PR.

- M14: `deploy/privateloop/private-mode.sql.candidate`, SHA256
  `20a4c1b1f7ae9f968984d8c12aba80f3e52a5d85a126c96c21bd953aa9c18a0a`.
  Adds execution_mode/`private-isolated`, owner helper + Worker boolean EXECUTE;
  replaces ten exact predicates. It does **not** define a real-Internet Gate.
- M15: `deploy/capacity/continuous-capacity.sql.candidate`, SHA256
  `78a2ac0e388ea0d4fb46e046b624315a88f07b82b58fe5784e246815197fa212`.
  Depends on exact M14; Reporter NOLOGIN group, two private tables, two Reporter
  EXECUTEs and API definer owner's boolean EXECUTE; seven bounded function
  replacements. Worker/Controller/Supervisor gain no additional privilege.
- `runtime-identities.sql.proposed`: precise **unapproved/unapplied** five LOGIN
  proposal (two Workers, Supervisor, Controller, Reporter), existing group
  membership/CONNECT only,2s/2s/250ms defaults. No new function/table/column/DDL/
  TEMP/API-owner/server-file privileges. It supersedes the still-unapplied PR21
  single-Worker LOGIN proposal; do not apply both. Passwords, private modes,
  incarnations/expiry, Supervisor public-key hash, target catalog generation,
  Reporter postmaster/device/FSID/generation are separately bound offline to the
  chosen asset; this PR supplies no fabricated registration or opening UPDATE.

Prior M14/M15 approvals are disposable/no-upstream ONLY. Their production
application and these LOGINs require separate exact approval. Gate stays closed
with emergency_stop=true. There is no real-Gate/egress activation SQL/command in
this bundle; applying old candidates alone cannot enable real private monitoring.
See the existing PERMISSIONS.md/permission manifests for unchanged exact functions.

## Three remaining P0/P1 blockers only

1. **P0 — missing implementation:** real target admission + DNS/routed egress
   integration. Web URL save/Owner consent is not target admission. Current
   launcher restricts `a.fixture.example.com`/two fixture IPs, kernel creates
   network-none veths to that receiver, and Probe/network policy use fixed
   `192.0.2.53`/`fd64:53::53` fixture resolver exceptions. These are code limits,
   not production settings. Next minimal work must bind an operator-approved
   invited target to the existing catalog/rotation/Permit, obtain full validated
   A/AAAA/CNAME answers, bind the same literal to the independent expiring
   default-deny gateway/run rules, then use the original TLS/one-GET/Result/stop
   path. No arbitrary user URL, host default route, DNS redial, proxy/redirect/
   retry or fixture Gate is an acceptable shortcut. Existing SSRF/physical
   mechanisms should be reused; no generic proxy or new scheduler is needed.
2. **P0 — pending production authority:** exact M14/M15, five LOGINs and concrete
   asset/identity/catalog/storage/key registration are not production-approved.
   A future real execution mode/Gate requires its own implemented, reviewed
   forward diff; `private-isolated` must never masquerade as Internet mode.
3. **P1 — host acceptance, mainly configuration:** bind one dedicated native
   Linux server, VPN/DNS/certificate, persistent storage/backups and secret/key
   ownership; prove invited access and rejected noninvited/XFF/IPv6 bypass, plus
   independent default-deny outbound/host-control exclusions on that asset.
   Local render/mocked metadata/isolated CI do not prove these host facts.

Historical125ms/55P03/IPv6/management-ACK unknown causes and all failed PR25
artifacts remain retained, not declared repaired by unrelated green tests.
Pressure/scalability work remains P2; do not add it to these three blockers.

Verification: `python3 -m unittest discover -s tests/release -v` and
`python3 tests/release/check_compose.py`. Tests use temporary public certificates
and dummy secret files, never a database, workload or Internet target. CI runs
only these directed release checks in addition to the repository's automatic Go CI.
