# Zero target network acceptance

From the repository root, with the repository Go toolchain and Linux Docker:

```sh
MONITOR_AUDIT_OUTPUT=/absolute/disposable/evidence tests/network/run-monitor-audit-docker.sh
```

The host wrapper builds the audit binary with `GOTOOLCHAIN=local`, explicit `GOOS=linux` and Docker's architecture, and the pinned Goose3.26.0. It creates its own disposable PG18 container without host ports, runs the audit in that container's network namespace, and removes only the container it created. No application database, developer data volume or host firewall is used. Container build/dependency download precedes the audited interval. NET_ADMIN changes only this disposable namespace; SYS_PTRACE enables syscall tracing. No host network or privileged container is needed.

The lower-level `run-monitor-audit.sh` is used by CI after normal integration and integration-race. CI builds the dedicated `integration,networkaudit` test binary and uses its disposable PG18 service namespace. Both runners install IPv4/IPv6 egress rejection before executing tests and restore their named rules afterwards. `strace -yy` also identifies socket protocol, so UDP to an otherwise approved port is not exempt. Only TCP127.0.0.1:5432 (test DB) and TCP127.0.0.1:18081 (HTTP fixture), plus established replies, pass the firewall. The syscall classifier independently allows only those exact destination tuples; it never ignores a connect because it failed.

1. `TestMonitorNetworkProbePositiveControl` attempts TCP192.0.2.1:80. The namespace firewall prevents egress. The child intentionally exits0 after the attempt fails; the classifier MUST record that attempt and exit1. The harness requires both that exact destination and exit1, so an unrelated failure cannot pass calibration.
2. `TestMonitorNoTargetNetwork` runs the real HTTP journey: registration/login, Project creation, invitation-backed Member join, Monitor POST/list/detail/PATCH, If-Match failure/replay, cross-tenant substitution, membership exit and logout. Only DB/local HTTP connections are approved. The classifier MUST exit0 with zero unapproved attempts and nonzero approved attempts, showing the workload actually ran.

These tests exist only under the dedicated `networkaudit` build tag. They fail if invoked without the isolated-runner environment. The ordinary integration suite does not pretend it ran the calibrated audit.

Evidence files contain the commands, test output, child/audit exit codes, classifier summaries and complete syscall traces. CI uploads the evidence artifact even on failure. Trace format is checked fail-closed: IPv6, Unix sockets and unknown destination formats are rejected; explicit `sendto`/`sendmsg`/`sendmmsg` destinations are also audited. NULL destinations use sockets whose earlier connect was traced. Reproducibility depends on Linux strace/iptables/Python; the image pins the same PG18 base digest as CI.

This proves configuration operations do not attempt target networking in the tested workload, supported by the production-source import/call guard. It does not prove a future prober is SSRF-safe. DNS rebinding/IP pinning/TLS/proxy/redirect/egress contracts for an actual probe engine require separate implementation and acceptance.
