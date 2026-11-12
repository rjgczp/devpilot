# Private Worker foundation — always closed

This is a separate Linux `cmd/privateworker` program, not a renamed fixture/test
binary. The existing Store, Owner/Start/Fence/Result SQL, single-use input codec,
target permit and result types are shared by explicit `privateworker` and
`schedulerfixture` builds. Ordinary API builds still import neither execution
package. Only the fixture build has `NewFixtureWorker`; the private entry can
construct only `NewClosedWorker`. Database `fixture` gate and execution-looking
environment flags cannot turn it into an executor.

This round implements closed startup, real effective-LOGIN validation and safe
shutdown/restart, plus a common one-Claim/one-Start Worker driver used by the old
fixture. **It cannot poll/Claim/Start jobs or launch a Probe in private mode.**
There is no enable flag, privileged socket, public route manager, fake release,
automatic Claim/Start retry or reconstruction of a started Run. A validated
frozen Result can still use the original commit/reconcile path while admission
is closed; SQL continues to require independent physical release first.

## Minimal native Linux use

Build only (does not provision a role, network, gate or database):

```sh
sh scripts/private-worker.sh build
# Equivalent native build:
CGO_ENABLED=0 go build -tags=privateworker -o privateworker ./cmd/privateworker
```

After separate approval/provisioning of a restricted LOGIN, choose an **existing
internal PG-only Docker network**, its stable offline Worker incarnation UUID,
and an absolute secret-file path containing that LOGIN's PostgreSQL URL:

```sh
export WORKER_CONTROL_NETWORK='YOUR_EXISTING_INTERNAL_NETWORK'
export WORKER_INCARNATION='YOUR_OFFLINE_WORKER_UUID'
export WORKER_DATABASE_URL_FILE=/private/0700-parent/worker-url
sh scripts/private-worker.sh check
sh scripts/private-worker.sh run
```

The candidate account is not provisioned by these commands. No production
deployment or network activation is authorized by this example. A fresh private
management DB does not yet have a Worker account, so `run` cannot work there
until the candidate is reviewed and credentials are provisioned offline.
Incarnation format is checked but closed startup is not proof of an activated
scheduler identity, a verified Supervisor or a usable private execution gate.

Use only a dedicated Worker DSN, with password and explicit database/host; no
credentials in argv or environment. The Compose service mounts only that file,
not API/admin credentials, cloud files, source, Docker socket or signing keys.
Host parent0700/file0444 permits its file bind mount to be read by UID65534
without exposing the host parent to other users; deployment ACLs still require
an actual host review. The image is scratch/CGO0, root read-only, caps0,
no-new-privileges,256MiB/CPU1/PID128/FD256. The process checks actual UID/caps/NNP,
rejects usable IPv4/IPv6 default routes and credential-bearing environment, and
requires PG18, exactly the existing Worker membership and no API/admin, DDL/TEMP,
secret-table or Run/control-update authority. The one-connection pool enforces
2s statement/transaction and250ms lock limits. Errors redact DSN and file path.

Readiness is `worker_ready`, `gate=closed`, `execution_available=false`.
SIGTERM/interrupt exits cleanly with `physical_release=false`; an exit log or
container deletion is never a physical proof and never returns a slot. There
is no health HTTP listener or automatic restart policy. A second invocation
starts closed again and does not inspect/resend any prior GET.

## Permission/schema review

`runtime-login.sql.candidate` is **inactive** and outside `migrations/` and the
image build context. Exact proposed changes: one `devpilot_private_worker` LOGIN,
CONNECT to the dedicated DB, membership in the existing
`devpilot_scheduler_worker`, and2s/2s/250ms session defaults. Existing private DB
PUBLIC TEMP/CREATE restrictions must remain. Login authentication and a future
trusted incarnation are separately provisioned; no password is embedded here.
The API role cannot be reused because it lacks Worker functions and has business
data access a Worker must not hold. No existing role gains a privilege; no new
function EXECUTE, field, table, index or gate value is proposed in this round.

The candidate has **not** been applied, including in tests. Integration reuses
the original fixture Worker LOGINs/ACLs, tightening only disposable PUBLIC TEMP.
Migration1–10,12,13 remain unchanged;11 stays absent. A future private admission
gate, fresh Owner confirmation, trusted target catalog, public resolver/egress
and actual Supervisor deployment require a separately reviewed forward diff.
Never open `fixture` to deploy this command.

## Targeted verification

Requires Docker, Python3 and OpenSSL3 with ED25519 support:

```sh
sh tests/privateworker/run.sh /absolute/new/test-output
```

Linux Go1.24.3 builds verify unit/race, unknown Claim/Start commit handling and
ordinary API dependency isolation. The existing no-upstream PG18/dual-stack
receiver fixture produces a real GET using its original Worker/Probe/Supervisor.
While that Run remains physically held, the new private executable starts,
stops and restarts against the existing restricted Worker LOGIN. Receiver count,
Run/Fence/Owner and reservation/budget facts remain identical. Forged physical
stop assertions and direct Worker release attempts are rejected. Only the
original independent Supervisor then stops the Probe and settles proof; three
real60s cycles complete with minimal Results/history projection/due checks.
The original fixture entry is also run in normal mode. There is no public
target, new grant, changed timeout, historical full matrix or capacity claim.

Runtime data-plane execution remains unavailable. Future public egress,
Supervisor activation/stop proof and private Gate review remain P0; continuous
retention/storage admission remains P1; extreme/load/storage stress is deferred.
OpenSpec stays37/47. No deployment, production migration or Archive is performed.
