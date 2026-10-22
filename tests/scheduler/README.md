# Isolated scheduler acceptance fixture (implementation in progress)

This directory is a test harness for approved plan A. It is not a production
worker distribution or permission to probe internet targets. Ordinary builds
exclude `cmd/schedulerfixture` and `internal/scheduler`, including on Linux.
The API dependency graph excludes both scheduler and execution kernel; its only
background worker remains Session cleanup. Environment strings cannot grant
execution capabilities.

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

Full `run-docker.sh`, independent Supervisor and periodic/fault acceptance are
still being implemented; preflight success must not be reported as plan A done.

Task6.4's independent network-policy component and dedicated acceptance are
described in [network64.md](network64.md). It uses direct veth pairs between
network-none namespaces, not the earlier bridge experiment or a production
network. Its raw-socket witness does not replace the approved execution kernel;
physical termination/release and combined6.3 acceptance remain outstanding.

Checkpoint1 build boundary: the existing network Probe is exclusively the
`TestExecutionProbe` entry in `internal/monitoring/execution_network_test.go`,
selected only in an explicit Linux `executionnet` test build. Its snapshot wire
bridge is also a `_test.go` file. Neither enters ordinary library GoFiles or API
artifacts. The Linux `schedulerfixture` command currently refuses startup; a
periodic controller, per-run scheduler Probe bridge and Supervisor are not yet
delivered. `TestReleaseArtifactAndProbeTestBoundary` inspects a real ordinary
Linux API binary, including with execution-looking environment flags. These
boundaries do not constitute the later per-process credential/permit acceptance.

The API zero-target audit retains the two exact TCP destinations and includes
finite authorization plus authoritative status HTTP/PG scenarios. No scheduler
target is added to that whitelist. CGO provenance and NSCD counterexamples stay
in the original classifier/build-gate tests.

Task6.5's independent physical-stop and recovery fixture is documented in
[stop65.md](stop65.md). It verifies existing TLS-flow cut, pidfd termination of
all Run children, signed full-lineage receipts and failure quarantine. It does
not release database capacity or complete5.4/6.3.

Task5.4 physical quarantine/receipt settlement and conservative recovery: [release54.md](release54.md). Separate capability approval and cryptographic/SQL trust boundary: [capabilities](../../docs/monitor-scheduler-task54-capabilities.md). Task6.3 remains incomplete.
