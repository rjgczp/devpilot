# Checkpoint3: shared database budgets (tasks5.1–5.3)

This checkpoint implements only database budgets and the offline control-plane
fixture directory. It sends no target traffic and creates no Probe, namespace,
network rules or physical termination receipts. Ordinary builds have no runner
entry point. Task5.4 stays unchecked, pending independent group6 acceptance.

## Atomic resource ledger

| Resource | Concurrent slots | Start/permit rate | Burst |
|---|---:|---:|---:|
| Global |32|8/s|8|
| Workspace |4|1/s|2|
| Project |2|1/2s|1|
| Monitor |1|max(persisted not_before,last Start + current interval)|one cycle|
| Canonical origin, across tenants |1|1/60s|1|
| Selected native IP, across origins |2|1/s|1|

Claim reserves global/W/P/M concurrency and terminal capacity with conditional
updates while holding their rows in order. Start consumes global/W/P tokens.
A failed transaction rolls back all its partial updates. A committed Start is
never refunded by PATCH, pause, target refusal, an uncertain ACK or a restart.
Monitor schedule and budget cooling remain durable through configuration/epoch
changes. Start also checks last_started_at + the **current** interval, so raising
an interval cannot use an old shorter boundary. Inserts enforce the total10,000
budget-key limit atomically; retention_capacity maps to a fixed internal error.
No admission uses an unlocked COUNT.

Tokens use integer millionths and numeric elapsed database seconds. Origin
refill divides by60 before rounding, avoiding an integer1/60 truncation. Private
arithmetic tests exercise one microsecond before, exactly at, and before the
last-refill clock. Real Start tests separately exercise competing connections,
8/s burst8, W burst2 and a real two-second Project refill. Origin tests wait a
real sixty seconds; IP tests wait real one-second intervals. These DB tests do
not attest target network delivery.

## Target reservation and catalog

Two precise EXECUTE additions were explicitly approved; see
`docs/monitor-scheduler-checkpoint3-capabilities.md`. No runtime table/column ACL,
role attribute, secret-reading privilege or physical-release function is added.
The Controller now has a registered control-row kind, but its only new command
is catalog initialization/rotation. Both functions bind session_user to a trusted
incarnation and enforce existing READ COMMITTED2s/250ms bounds.

The fixture-only Go directory copies its HMAC secrets into unexported storage,
redacts generic formatting, and performs only offline canonicalization/membership
checks. Origin HMAC domain-separates scheme, lowercased host and normalized port;
IP HMAC domain-separates the native address family and bytes. Paths never enter
hash inputs. No resolver/dial is called. Bind rejects an IP absent from the
immutable fixture directory; it cannot choose a replacement after DNS approval.
The integration configuration uses reserved fixture.example.com names accepted
by the existing API profile, without resolving or connecting to those names.

The trusted catalog binds digest pairs to full W/P/M/configuration-version
identities. Its maximum is1000 entries/512KiB, with at most current+previous key
generations. This bounds the SQL input, not the number of registered Schedules;
the existing10,000-Schedule cap remains. Subsequent rotations preserve all
existing scoped identities and their current digest mappings. A newly registered
scope/version must carry both the previous and new control-plane HMAC digests,
so sharing an existing target still shares its cooling across tenants/versions.
The2s database budget remains mandatory even for a large catalog.

Reservation uses Owner User SHARE → Workspace NO KEY UPDATE → Membership →
Project → Monitor → Activation → Schedule → Run → global/W/P/M → all sorted
origin keys → all sorted IP keys → Reservation → control/registered Supervisor.
Rotation takes global → sorted target keys → control, and never then takes a
scoped parent/User. The global lock serializes catalog changes with target
reservation, so the catalog read preceding target-row locks cannot change.
Fresh statements after all waits recheck current Owner/membership id, full scope,
v/g/epoch/fence, auth expiry, lease horizon, consumed input, Supervisor registration,
permit hard deadline, deployment/stop and HTTP intent-v2 gates. One saved final
DB clock is reused for every update. Rejected stages roll back all target slots
and tokens; they do not manufacture a target HTTP failure.

A successful reservation records all retained digest pairs, one permit identity
and deadline. It does not install network permission. Commit uncertainty returns
no permit; repeated requests cannot mint another. A future target-permit hook
must wait for confirmed DB commit **and** independent Supervisor rule installation
before approving the original executor's literal-IP dial. That hook is group6,
not part of this checkpoint.

## Rotation and quarantine

Rotation requires all global/target concurrency drained. It cannot release or
reset a budget. Existing scoped mappings are retained under old+new generations;
both participate in each reservation, so a new digest cannot clear old cooldown.
Old rows keep their original tokens/not_before and keep_until at least one hour
past rotation. Another rotation is refused for that hour; no eviction of retained
or associated keys is implemented. Tests assert database deadlines and refusal
before one hour; they do not claim a one-hour wall-clock soak.

Each reservation stores every retained key, enabling future release accounting
to release the actual generations it reserved. Deletion guards recognize these
retained references. Started/unknown capacity remains held absent independent
physical proof, regardless of lease/ACK/time. This checkpoint introduces no
release API. A future protocol must bind Supervisor + namespace incarnations,
revoked network permit, cut established flows, process exit and non-recoverability.
Receipt verification and physical release acceptance remain incomplete5.4.

## Reproducible tests

Build Linux binaries with `integration,schedulerfixture,budgetfixture` and run:

```
python3 tests/scheduler/checkpoint3-pg.py --binary /private/tmp/devpilot-scheduler-tools/cp3-budget.test --evidence /private/tmp/devpilot-cp3-final --name budget-normal
```

Use the corresponding `-race` binary for race acceptance. Each top-level budget
test gets a fresh pinned PG18 container on the existing internal control network,
no published ports, no added capabilities/privilege and no target networking.
The existing testdb guard plus unique cluster_name rejects accidental reuse of
another database. The harness captures exact command, source/binary/log hashes,
SQLSTATE, pg_locks/blocking graphs, final held resource ledger, PostgreSQL logs
and container configuration. It destroys only its freshly-created labeled PG
instance/anonymous volume after capturing the ledger. Tests assert Goose Down
refuses when physical proof is missing. Teardown never writes a termination
receipt or physical-release timestamp. Destroying a disposable database is not
physical Supervisor acceptance.

Historical integration regressions use their existing reviewed database fixtures;
their administrative teardown is not physical-release evidence. New group5 tests
do not use that teardown. Original protected PG18 regressions remain serial and
retain their time budgets. No network-security76-case result is claimed for this
checkpoint; that requires the independently authorized network stage.
