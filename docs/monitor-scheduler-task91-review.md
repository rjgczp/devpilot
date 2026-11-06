# Task9.1 review / MVP follow-up

Scope: isolated multi-instance periodic execution only;36/47 remains unchanged,
Task9.1 awaits independent acceptance. Task8.3 is deferred and unchecked. No
Migration11, production migration, deployment, merge, Archive or public egress.
Parent: `e969fafa272d1d33bab3a303bfaa1619f552ffc5` on the approved design branch;
accepted8.2 runtime remains `0462dc2e65c8ea9edd3948cf6a72d483e93f7510`.

## Implementation and reproducibility

Run `sh tests/scheduler/run-docker.sh /absolute/new/evidence both` from the
repository root; see [full instructions](../tests/scheduler/README.md).
Two separate UID65534 Workers reuse the original narrow LOGIN memberships,
Scheduler Store, kernel, network managers and trusted physical stop chain.
One real Owner HTTP authorization covers three actual60s cycles in each mode.
Normal uses IPv4; race uses IPv6. No shortened clock, manual due write, GET
retry, new DB role class or physical attestation source is introduced.

The first real run exposed a P1: successful Result Commit left next_due NULL.
The product owner separately approved **Migration12** to repair only this
completion transition in disposable PG18. It preserves Migration1–10 bytes,
the existing function owner/ACL/search_path/250ms lock limit and2s transactions.
The added UPDATE requires this Run's current full lineage, enabled finite
authorization, active Owner and existing fixture/deployment/HTTP gates. It
creates a new Run ID and due>=completion+interval; it never renews authorization,
releases resources or alters the immutable observation. Committed replay returns
before this UPDATE. Pause/PATCH/old generation cannot rearm a revoked schedule.

The runner verifies the12 down/up/up round trip, live-gate rollback refusal,
unchanged privileges and the original Migration10 lifecycle with explicit9/10
boundaries. Existing ACK/idempotence, Pause and PATCH Result tests are reused in
normal/race. No historical full matrix is claimed as this SHA's result.

## Review evidence

The final frozen-SHA command receipts, raw three-cycle Run/Result/due/GET timeline,
binary/source hashes, signed stop bundles and CI artifact index are delivered
outside Git under the Task9.1 artifact directory. Read `<mode>/cycles.json`,
`worker*-identity.json`, namespace/firewall/process/trace records and the command
receipt exit codes. A final SHA is identified by its own receipts, never by an
earlier green run. `development-*` directories retain original failures,
including the NULL-due reproduction and harness startup/witness corrections.

Historical125ms global-lock wait, IPv6 and management-confirmation timeouts
remain root-cause-unknown. Disk measurements/1GiB budget/independent10GiB disk/
80% actual-disk denial remain unverified; current disk_used_bytes is not a
trusted live storage measurement. Those P2 items do not certify capacity or
authorize production execution.

## Short MVP gap checklist

| User operation | Existing implementation | Next actual gap |
| --- | --- | --- |
| Register/login/Workspace | Existing API, sessions and tenant membership | Connect a local Web UI; reuse contracts |
| Monitor create/edit/list/detail | Existing Owner/Member APIs | Delete is currently405/spec-forbidden; needs a scoped product/spec change preserving active-Run termination |
| Enable/pause/status | Existing finite authorization, activation and execution-status APIs | Dashboard controls and clear in-flight/unknown display |
| Periodic HTTP checks | Approved fixture-only Scheduler/Probe/Supervisor | Interactive isolated Demo wiring; real internet execution remains separately gated |
| Persisted results/history | Minimal Runs persisted; current/latest status exists | Authorized bounded history API and history view |
| Web Dashboard | No frontend/package manifest in this checkout | Small UI using existing auth/Workspace/Monitor endpoints |
| Local demonstration | Existing API+PG docs and new isolated runner | One coherent user-operated demo flow, without a production execution switch |

After9.1 review, the next proposed development task is a minimal local Dashboard
for login → Workspace/Monitor list → create/edit → authorization/pause → status.
Delete/history can be separate small scoped changes;9.2–10.5 are not automatic
prerequisites for this UI. No additional MVP feature is implemented this round.
