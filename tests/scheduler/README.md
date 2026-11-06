# Isolated scheduler acceptance fixture

This directory is a test harness for approved plan A. It is not a production
worker distribution or permission to probe internet targets. Ordinary builds
exclude `cmd/schedulerfixture` and `internal/scheduler`, including on Linux.
The API dependency graph excludes both scheduler and execution kernel; its only
background worker remains Session cleanup. Environment strings cannot grant
execution capabilities.


## Task9.1: repeatable real-time periodic fixture

From the repository root, with Docker running and Python3/OpenSSL installed:

```sh
sh tests/scheduler/run-docker.sh /absolute/new/task91-evidence both
```

`normal` selects IPv4 and `race` selects IPv6; `both` is the default. Each mode
uses a fresh guarded PG18 cluster, two independent Worker containers/LOGINs,
and **three complete cycles with the real60s interval**. Expect about3 minutes
per mode plus build time. The interval, due, authorization generation and DB
clock are never shortened or rewritten by the harness. Evidence directories
must not exist; any missing dependency, skipped test, failed assertion or missing
management acknowledgement returns nonzero. The runner does not retry failures.

Preparation builds the existing tooling image from pinned PG18 and Go1.24.3
images and Goose3.26.0. Only preparation may retrieve packages. The two named
`devpilot-fixture-gomod`/`devpilot-fixture-gobuild` volumes cache build inputs;
no runtime container mounts them. The host keeps Docker and the existing
physical Supervisor signing key. Never point this runner at an existing DB.

Runtime topology:

- A newly created `--internal --ipv6` control bridge contains only disposable
  PG18, the test coordinator and two Workers. No host port is published.
- Workers have separate ephemeral passwords, UID65534, caps0, no_new_privs,
  read-only tools,256MiB/CPU1/PID128/FD256 limits. They receive no API/admin or
  Supervisor credentials. Their original two restricted LOGIN memberships,
  EXECUTEs, table/column ACLs and2s/250ms budgets are reused.
- The coordinator uses a real Owner Cookie/CSRF/strong If-Match HTTP request
  for one900s authorization. It retains the Supervisor DB identity; a fixed,
  lineage-checked consume_input pipe adapter gives Workers no SQL or password
  capability. Actual network/stop proofs still come only from the unchanged
  trusted physical Supervisor, not a Worker report or container removal.
- Each Run gets fresh network-none namespaces/veths and independent run/gateway
  default-deny managers. The dual-stack DNS/TLS receiver has no upstream.
  The original Go kernel/Probe and signed pidfd/reap/namespace-retirement chain
  are reused. Probe has no source/config/DB mount, Docker socket or PG route.

The assertions cover completed Run/HTTP200/minimal Result/fixed commit identity,
unchanged configuration/Owner generation and Activation ETag, increasing fence,
next due at least DB completion+60s, one receiver GET per full Run lineage,
rejected replay of the same envelope, and physical release before Result.
Both Workers poll and compete in every cycle; which Worker wins is not fixed.
No transaction or pool connection spans network/management operations.

Migration12 is a separately product-approved **completion scheduling fix**:
Migration1–10 stay byte-identical, Migration11 storage work stays absent. It
only advances next_run_id/due after the existing Result Commit prerequisites
and current authorization/gates pass; it grants no role or privilege. The first
real integration run exposed completed Run + NULL due; original logs are kept.
Down restores the accepted function and rejects a live deployment/unreleased Run.
The runner checks12 down/up/up, unchanged ACL/owner/search_path, and the original
Migration10 lifecycle at its explicit9/10 version boundaries. It also reuses
three affected Result regressions (ACK/idempotence, Pause, PATCH) per mode.

Evidence: `build.json/log` and `periodic-*.json/log` bind command, exit, source
inventory and binary hashes; `<mode>/cycles.json` has the three raw Run/Result/
schedule/GET/accept/lineage records. Per-cycle directories retain routes,
firewall, receiver, trace, cgroup, process and signed stop proofs. Worker
identity/container records distinguish OS containers and PG backends. Capability
frames and ephemeral DB passwords are redacted. `manifest.json` hashes files.
Teardown removes only labelled resources created by that invocation; deletion
is never treated as physical-release proof. After interruption, inspect the
saved commands/label before removing only that invocation's resources.

This validates isolated multi-instance periodic behavior only. Production
internet execution remains hard closed. Task8.3 is deferred: disk_used_bytes is
not a verified live measurement, and this is **not** proof of1GiB capacity,
10GiB filesystem isolation or80% real-disk denial. Historical125ms lock wait,
IPv6 and management-timeout causes remain unknown; later success does not resolve
them. Task9.1 was independently accepted at00569eb;37 of47. Task8.3 stays deferred.

## Existing component fixtures

Build the trusted test tooling image before beginning an isolated interval:

```sh
docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
python3 tests/scheduler/preflight-docker.py --evidence /absolute/dedicated/evidence
```

Preflight fails for missing Linux tools, IPv6, network management permissions,
upstream links/routes, ineffective kernel permit expiration or Probe privileges.
It runs two concurrent Docker-created network namespaces. Inside each, a trusted
NET_ADMIN process installs dual-stack default deny before a UID 65534/caps 0/
no_new_privs child. A timed nftables set cuts an established TCP flow without
an ESTABLISHED bypass. This is a tool/capability check, not the completed
Supervisor, full topology or three-cycle scheduler acceptance.

The trusted outer harness uses Docker to allocate/destroy namespaces because
`ip netns add` inside a NET_ADMIN-only container requires additional mount
privileges. Probe never receives a Docker socket, NET_ADMIN, host mount or DB
credentials. Do not add SYS_ADMIN to Probe to work around this distinction.

PG18 destructive tests use `internal/testdb` before Goose and require the exact
isolated name `devpilot_registration_test`, a distinct `DATABASE_URL`, PG18 and
Goose 3.26.0. No production database or existing application container may be
used. On Docker Desktop, an internal bridge can suppress host port publication;
run CGO=0 Linux test binaries on the isolated control network instead of adding
an upstream route merely to make host-side clients connect.

The Task9.1 runner above composes accepted components; preflight alone does not
prove periodic behavior. Task9.2–9.5 fault/load/full-network acceptance remains
separate and is not claimed by this command.

Task6.4's independent network-policy component and dedicated acceptance are
described in [network64.md](network64.md). It uses direct veth pairs between
network-none namespaces, not the earlier bridge experiment or a production
network. Its raw-socket witness does not replace the approved execution kernel;
the accepted physical/joint components below provide the separate stop chain.

Checkpoint1 build boundary: the existing network Probe is exclusively the
`TestExecutionProbe` entry in `internal/monitoring/execution_network_test.go`,
selected only in an explicit Linux `executionnet` test build. Its snapshot wire
bridge is also a `_test.go` file. Neither enters ordinary library GoFiles or API
artifacts. The Linux `schedulerfixture` command currently refuses startup; the
fixture-only runner above supplies periodic orchestration without enabling that
ordinary command as a production Worker. `TestReleaseArtifactAndProbeTestBoundary` inspects a real ordinary
Linux API binary, including with execution-looking environment flags. These
boundaries do not constitute the later per-process credential/permit acceptance.

The API zero-target audit retains the two exact TCP destinations and includes
finite authorization plus authoritative status HTTP/PG scenarios. No scheduler
target is added to that whitelist. CGO provenance and NSCD counterexamples stay
in the original classifier/build-gate tests.

Task6.5's independent physical-stop and recovery fixture is documented in
[stop65.md](stop65.md). It verifies existing TLS-flow cut, pidfd termination of
all Run children, signed full-lineage receipts and failure quarantine. It does
not by itself prove database settlement; use the composed runner for that.

Task5.4 physical quarantine/receipt settlement and conservative recovery: [release54.md](release54.md). Separate capability approval and cryptographic/SQL trust boundary: [capabilities](../../docs/monitor-scheduler-task54-capabilities.md). Its historical scope remains distinct from the current composed acceptance.

### Task7.2 legacy network protocol retirement

Current schema8 retains `persist_minimal_result` for control stop reasons only;
network calls fail permanently with `0A000 legacy_network_result_disabled`.
New fixture code depends on `scheduler.ResultWriter`: typed control outcomes,
fixed-ID CommitResult and ReconcileResult. No result API calls the Probe.
The SQL gate also rejects direct calls by an otherwise fully authorized Worker.
See `docs/monitor-scheduler-task72-capabilities.md` for the authorized BREAKING
fixture transition and the no-replay, independent-physical-stop requirements.

`run-result71.py` now runs the current control branches (3 normal +3 race).
`run-result72.py` runs 9 normal +9 race, including genuine physical proof followed
by rejection of the old network SQL and success of the fixed-ID path (one GET).
Historical network assertions are not edited or silently skipped:
`run-legacy-result71.py prepare/build/run` checks out exact historical commit
`c1c9fe4b24efb2a4bc18de0ef4d75c9fe0f1014c` outside this checkout, builds that
component and executes its original 24-cell matrix with its own schema8 migration
and test harness. Goose is pinned3.26.0; Linux fixture build tags are unchanged.
Every Run gets a fresh guarded PG18 and no-upstream namespace. The history's
inner receipts identify c1c9fe4; current outer receipts identify orchestration.
`*-source.json` and `binaries.json` permit checking both, without claiming old
component results establish current implementation behavior. Never pair those
historical binaries with current migrations. CI fails if the pin is unavailable,
source is dirty, any test fails, or historical count is not24; no retries/skips.
This tooling is under tests only, never a release/production migration path.
