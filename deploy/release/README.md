# Private beta release preparation

The management stack and private-approved runtime share the existing API, Dashboard and PG18. The preflight is read-only. A named server, database approval, dedicated egress and deployment authorization are still required. OpenSpec remains 37/47.

## One preflight entry

```sh
cp deploy/release/release.example.json /absolute/private/release.json
# Edit references to the already prepared nonsecret management .env, private
# secret directory, existing service.json and persistent journal; create nothing
# with this checker. For the dedicated uplink use the routed fields below.
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
false in this revision. Secret files receive metadata checks. The routed preflight
hashes the two designated public PEM files and uses offline OpenSSL for the management certificate. Files0444 are allowed
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
require separately authorized host acceptance. No target request or SQL query
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
| PG18 + migrator / API identity | dedicated persistent volume; tools profile only; API gets its own credential | reviewed offline M13→M14→M15→M16 and identity binding; never on normal startup |
| Two Workers + Supervisor + Controller | `scripts/private-service.sh`; real standalone binaries/anonymous IPC, original stop proof; Controller includes retention | five separate LOGIN files, offline incarnations/expiry/catalog, pinned public key |
| Reporter + independent watchdog | actual metadata,3s monotonic guard,5s SQL sample;1GiB relation/80% gate unchanged | device/FSID/postmaster binding, readonly metadata paths and actual content EACCES |
| Private Probe / network managers | one GET, DNS/IP/Permit/literal/TLS protections already implemented | dedicated routed holder and server acceptance below |

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
then candidates 14 → 15 → 16**. 11 remains absent. No candidate is in
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
- M16: `deploy/targetadmission/approved-mode.sql.candidate`, SHA256
  `264ddb9bcb8bf48cf305bc81d2ce58434f0e0ff2dac9ad5f2b45ae0210fcf095`.
  Requires the locked M14 and M15. Adds the private-approved Gate/mode value,
  replaces three mode predicates and grants the existing Worker group only
  EXECUTE on `approved_worker_ready(uuid)` (PUBLIC revoked). No new roles or
  table/column grants. See [M16 permissions](../targetadmission/PERMISSIONS.md).
- `runtime-identities.sql.proposed`: precise **unapproved/unapplied** five LOGIN
  proposal (two Workers, Supervisor, Controller, Reporter), existing group
  membership/CONNECT only,2s/2s/250ms defaults. No new function/table/column/DDL/
  TEMP/API-owner/server-file privileges. It supersedes the still-unapplied PR21
  single-Worker LOGIN proposal; do not apply both. Passwords, private modes,
  incarnations/expiry, Supervisor public-key hash, target catalog generation,
  Reporter postmaster/device/FSID/generation are separately bound offline to the
  chosen asset; this PR supplies no fabricated registration or opening UPDATE.

Prior M14/M15/M16 approvals are disposable/no-upstream ONLY. Their production
application and these LOGINs require separate exact approval. Gate stays closed
with emergency_stop=true during provisioning. Applying the candidates alone cannot enable execution.
See the existing PERMISSIONS.md/permission manifests for unchanged exact functions.

## Dedicated uplink and startup

The deployment overlay builds the existing Dashboard for invited users. Its
status and history still come from the API. The Worker, Supervisor, Controller
and Reporter start together using the existing private-service entry. An optional
network-none holder carries a dedicated physical or PCI-backed virtio NIC. The
VPN/management NIC, a Docker bridge, host forwarding, and the host default route
are unsuitable. The read-only preflight can inspect an already prepared holder.

On an approved native Linux host, render both configurations first:

```sh
export PRIVATE_RUNTIME_IMAGE="$(docker image inspect devpilot-private-runtime --format '{{.Id}}')"
docker compose -f deploy/release/compose.uplink.yaml config
docker compose --env-file deploy/private/.env -f deploy/private/compose.yaml \
  -f deploy/release/compose.management.yaml config
```

After **host/network provisioning approval**, use the same Compose files for a
new stack. Create only the disconnected holder with
`docker compose -f deploy/release/compose.uplink.yaml up -d uplink`. Record its
full container ID from `docker compose -f deploy/release/compose.uplink.yaml ps -q uplink`
and resolve the full ID with `docker inspect --format '{{.Id}}' CID`.
An operator binds the dedicated data-plane NIC to this namespace as `uplink0`,
with its actual MAC and PCI slot. The physical port must terminate on the
reviewed upstream VLAN/router. Provision only the following on this NIC:

- Configure static source addresses with /32 or /128 and `noprefixroute`. Disable
  automatic link-local address generation, RAs, redirects and DHCP. Set permanent
  neighbors on both ends (forwarding policy drops local input and output).
- Add an explicit IPv4 host route for `192.0.2.53/32` and a host route for the
  **single approved target literal**, IPv4 /32 or IPv6 /128. For example,
  `ip route add 192.0.2.53/32 via "$NEXT_HOP4" dev uplink0 onlink` **inside the
  holder**, with separately assigned link and neighbor addresses. For an IPv6
  target use `ip -6 route add "$TARGET6/128" via "$NEXT_HOP6" dev uplink0 onlink`.
  No default route in any table. Admission rejects preexisting downstream links,
  local target aliases, foreign firewall tables and extra unicast routes.
- The independent trusted resolver must own or serve the dedicated wire endpoint
  `192.0.2.53:53/TCP` and resolve both A and AAAA using trusted upstream DNS.
  Publish no resolver on the management host. The independent upstream firewall
  allows only this resolver and the approved HTTPS literal, with explicit expiry.
  Install this separately on the named asset. Revoke it when approval ends.
- The launcher atomically installs the holder's default-drop input/output/forward
  filter before creating any per-run link. Only TCP DNS to the fixed wire and
  TCP/443 to the selected literal pass; SNAT uses the dedicated interface.
  The original run and gateway each require a time-limited Permit before HTTPS.
  The holder does not create a permitted Run, extend expiry or accept a new IP
  after a DNS change. Host/network administrators and NIC assignment are trusted.

Start with `deploy/privateservice/service.example.json`, applying the operator
approval instructions in [target admission](../targetadmission/README.md).
Set `mode=private-approved`, `target_container` to the holder ID,
`target_host` and `target_ip` to the approved name/literal, `approval_file`,
and `denied_prefixes` to actual management/VPN/control ranges for both families.
Add `routed_uplink` with exactly `pci_slot` and `mac`. Pin the SHA256 of the
actual **public PEM bytes** in `supervisor_key_sha256` and `target_ca_sha256`
(the latter hashes `ca_file`). Keep target catalog and public approval in sync.
Re-review DNS with the operator CLI in a dedicated review namespace having only
the fixed resolver route. Register via the existing Controller catalog path.

Bind distinct incarnation UUIDs and DSN files for the five existing identities.
All names begin with `devpilot_private_`. Suffix → container UID mappings are
`worker` → 65532, `worker2` → 65533, `supervisor` → 65531,
`controller` → 65530, `reporter` → 65529 (sampler/watchdog). The proposal
in `release-lock.json` pins the exact role memberships. Each role reads only
its own DSN, with URL-encoded password, `postgres:5432/devpilot_private` and
`?sslmode=disable` on the internal-only PG network. No DSN or private key is
printed. Pin the Supervisor public key to its registered DB row. The offline
preflight checks local metadata, distinct files/UUIDs, certificate hostname and
expiry, and public pins; the effective LOGIN/registry and key pair still require
real-host verification. Verify the management TLS chain and key pair locally or
in a VPN client handshake before inviting users.

```sh
sh scripts/private-release.sh --manifest /secure/release.json --inspect-host --config-only
# Only after separate Gate, egress and deployment approval:
sh scripts/private-service.sh start --state /var/lib/devpilot-private --config /secure/service.json --run-routed
sh scripts/private-service.sh status --state /var/lib/devpilot-private
sh scripts/private-service.sh stop --state /var/lib/devpilot-private
```

Without the routed switch, a routed configuration cannot start. Stop new
admission first on rollback, revoke the upstream allowance, drain via the
existing Supervisor, preserve the journal and reconcile with PG. A missing
trusted receipt keeps capacity held and requires recovery. Retain the holder
and diagnostics while any Run lacks trusted physical termination.

## Remaining approvals and host acceptance

1. **Database and Gate.** On a dedicated PG18, independently approve the exact
   M14 → M15 → M16 sources, five-LOGIN proposal and registrations. Keep
   deployment closed/emergency-stopped during migration; require no unreleased
   Runs. Bind LOGIN, private-approved mode, incarnation, expiry, Supervisor key,
   target budget/catalog and Reporter postmaster/filesystem identities.
   Separately authorize a finite deployment Gate for private-approved and
   clearing emergency stop. The existing finite Owner consent is also required.
2. **Network.** Bind a real dedicated NIC and upstream ACL with named target,
   resolver, expiry and revocation owner. Verify the physical return path, both
   families, and denial after expiry on that specific asset before enabling.
3. **Deployment.** Approve the server, VPN-only access, management certificate,
   secret and persistent journal/storage mounts, backup, operator and start/stop
   window. Check invited and noninvited access, per-role effective identity and
   real Run/Result/history on the authorized asset.

Directed checks: `python3 -B -m unittest discover -s tests/release -v`,
`python3 tests/release/check_compose.py`, and after building the service image,
`python3 -B tests/release/routed_wire.py`. The latter uses three disconnected
Linux namespaces and an owned DNS/TLS peer; veth exercises forwarding, not
physical NIC identity or Internet reachability. Original failure artifacts and
unresolved 55P03, IPv6, timing and lifecycle-wrapper EOF evidence are retained.
