# Schema8 Activation representation and isolated scheduler rollout

This document describes the approved plan A protocol. The implementation is
still undergoing acceptance; no production network execution or migration is
permitted by a fixture result. Ordinary API builds have no execution kernel or
scheduler entry point. Fixture builds require Linux and `schedulerfixture`.

## Client cutover is a breaking change

Schema8 preserves the fourteen Activation fields, their JSON types, Owner
commands, permissions and strong `monitor-activation:<mid>:<v>:<g>` ETag format.
Successful GET/PUT responses carry
`DevPilot-Activation-Projection: intent-v2`. These fields are deprecated:

| Field | Schema7 | Schema8 |
| --- | --- | --- |
| `execution_state` | `blocked` | `not_reported` |
| `blocked_reason` | `scheduler_unavailable` | `execution_status_required` |
| `next_due_at` | null | null; use status `next_due` |

The two schema8 strings are migration hints, not actual execution observations.
Closed-enum clients must upgrade. They must never infer blocked, healthy or idle
from these strings, from `desired_state=enabled`, or from a missing status API.
The new execution-status endpoint is the sole authority. A missing interface,
404, 503, failed request or invalid response displays **运行状态不可用**. There is
no fallback to the deprecated fields. Activation remains a stable intent
representation: scheduling, heartbeat, completion and physical release do not
change its body, `updated_at`, generation or ETag.

Read Activation and status separately, compare full scope and current v/g, and
refresh authoritative status when they disagree. Do not combine reads spanning
a configuration or activation change as though they came from one snapshot.
Bound refresh attempts; continued changes mean status is temporarily unavailable.
Within one status response, authorization, scheduling, attempts and observations
come from a single database statement. `desired_state=disabled` may coexist with
an older `started` or `unknown` attempt and `in_flight=true`. A current unknown
attempt must not be hidden by an older completed success. `current_observation`
is null when its v/g/epoch does not match the current authorization.

## Upgrade order

1. Deploy client support for intent-v2 and authoritative status first.
2. Close new Start admission and drain every old HTTP instance, including read
   traffic and in-flight writes. Inventory every routable instance; an omitted
   or unverified instance is a failed gate. Drain old superuser connections and
   replace them with reviewed runtime identities.
3. Only the migration identity applies Migration8 in a maintenance window.
   Every existing Activation, enabled or disabled, advances g once. Intent is
   disabled, the old authorized version and actor are cleared, transition is
   `upgrade_revoked`, and cooling deadlines remain identical. No Schedule or
   Run is backfilled. Overflow of any generation aborts the entire migration.
   An up/up does not consume another generation.
4. Route traffic only to new HTTP instances. Independently check each instance's
   successful Activation header and fixed values, status route, authenticated
   Owner/Member authorization and client handling of status failures. Record the
   instance inventory and checks in the trusted Supervisor manifest.
5. Fixture admission may open only after the independent Supervisor verifies
   the isolated topology, default-deny rules, namespaces, resources and HTTP
   inventory. A routable old, expired, unknown or unready HTTP instance refuses
   admission. A database row, environment variable, container name or CI pass is
   never an isolation attestation. Production execution remains closed.
6. The Owner must explicitly authorize the current configuration v and
   activation g, using a strong Activation If-Match. Lifetime is 60–86400 seconds
   by database time; it is not automatically renewed. Legacy activation PUT can
   only set intent and revoke a grant, including enabled-to-enabled writes.

Pause uses Start Commit linearization: no new Start is approved under revoked
permission, but previously started requests may send or finish after the pause
response. A GET already delivered cannot be recalled. Session logout does not
become a background identity or renew authorization. New Claim/Start requires a
current active Owner, not the continued presence of the original author.

## Rollback order

First close new Start admission, revoke active grants, and independently stop
all physical executions. Confirm withdrawn permits, cut established flows,
terminated processes and permanently retired namespace/incarnation permissions.
Without this independent proof, capacity remains quarantined and rollback must
wait. Then drain all HTTP reads and writes. Retire every Activation tag before
switching representations; old ETags must never become valid again.

Goose Down is a disposable-test lifecycle operation, not this operational
rollback. It refuses an open gate or unconfirmed physical execution. It retires
all generations again, keeps intent disabled, preserves every cooling deadline
and never restores old enabled authorizations. A max-bigint generation causes
an atomic failure rather than tag reuse. Restarting a process or changing an
environment variable does not replace the rollback protocol.

## Database identity and transaction boundary

The non-LOGIN function owners do not become memberships of runtime LOGINs.
Worker/Controller do not read user password or session token hashes, perform
business DML, DDL or TEMP creation. System functions bind `session_user` to a
trusted registration and exact incarnation, with READ COMMITTED, statement and
transaction budgets at most 2 seconds and lock timeout at most 250ms.
Provision those settings on each real LOGIN; group-role defaults are insufficient.
Never reuse the migration or historical superuser identity at runtime.

Claim order is unlocked hint → candidate User SHARE → Workspace NO KEY UPDATE
SKIP LOCKED → Membership → Project → Monitor → Activation → Schedule → Run →
global/Workspace/Project/Monitor budgets → Reservation → control. Each candidate
uses a separate transaction; a skipped Workspace or invalid Owner aborts the
whole transaction before another Owner can be selected. Fresh SPI statements
and one post-wait `clock_timestamp()` gate all mutation. See the
[PostgreSQL18 timeout definitions](https://www.postgresql.org/docs/18/runtime-config-client.html)
for transaction and per-lock semantics. Actual lock-graph tests are required.

Run capacity reserves future terminal space before admission so loss of an ACK
or an unknown network outcome cannot leave an unauditable execution. It does
not refund Start rate tokens. Lease expiry cannot release physical slots.
These database rules do not by themselves prove Linux isolation or termination.
