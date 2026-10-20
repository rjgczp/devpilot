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
