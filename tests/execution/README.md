# Isolated single-execution acceptance

This runner is separate from `tests/network/`, whose configuration API policy,
exact whitelist and CGO/NSCD regression corpus remain unchanged. These tests do
not approve a production service, activate monitors or contact real targets.

From the repository root with Go1.24.3 and Linux Docker:

```sh
EXECUTION_EVIDENCE=/absolute/new/evidence sh tests/execution/run-docker.sh
python3 -m unittest discover -s tests/execution -p 'test_*.py' -v
```

Build/dependency retrieval precedes the audited interval. The wrapper builds a
CGO=0/Linux test binary with `executionnet`, verifies build metadata, and mounts
only the binary, fixture scripts and dedicated evidence directory. There is no
application source/config/database volume in the probe container. The container
uses `--network none`, IPv6, memory128MiB, CPU1, PID128 and nofile256. The outer
trusted harness configures a disposable namespace only; it never touches the
host firewall. Local fixture addresses emulate public/native IPv6, metadata,
private and translated destinations. All routes are namespace-local, with no
upstream default route or external NIC. Some Docker kernels expose inert tunnel
templates: they must be DOWN, have no addresses/routes, and remain unusable by
the unprivileged child. Unexpected interfaces/addresses/routes fail preflight.

DNS, HTTP and TLS servers are independent Python fixture threads/processes.
The kernel being tested is a separate Go test process. A numeric-only drop
wrapper removes all capability bounding bits, sets no_new_privs, drops groups,
GID and UID to65534, then execs the test with an explicit credential-free
environment. The child checks capabilities/FD limits. There are no cookies,
database credentials, cloud credentials, test private keys or production
targets in its input. TLS CA trust and snapshot serialization exist only in
`*_test.go`; no production injection API or execution entry is created.

Both the wrapper and Go executable are traced. The original setpriv prototype
was rejected on its own libc/NSCD Unix socket attempts, before the Go exec;
the numeric drop avoids that IPC rather than exempting it. Unix/unknown/UDP or
unapproved destination attempts still fail classification. Trace evidence has
process/exec records, not environment dumps or HTTP payload write calls.

Output files are per case: command, child/audit exits, actual connect attempts,
timing, test output and full syscall trace. `events.jsonl` separately records
fixture DNS questions, TCP accepts, TLS SNI and whether a GET/credentials reached
the endpoint. Fixture-only addresses/names in restricted security evidence are
not ordinary application logs; results contain no URL/path/headers/body/raw
errors. No real customer data is used. Rules/counters, namespace routes and
actual cgroup limits accompany the summary. Missing capabilities/tools/IPv6 or
unexpected trace formats fail rather than skip.

Normal blocked targets must have zero target TCP attempts; dedicated DNS is
counted separately. Rebinding requires two independent DNS connections but one
HTTP connection; repeated safe probes require two of each. Proxy env variables
are deliberately poisoned. Header-only success with an infinite/gzip/chunked
body must return promptly without reading/draining/decompressing it. T=1 tests
overall clipping; T=30 tests independently enforce DNS2/TCP3/TLS3/headers5.

Four deliberately malicious positive controls bypass the application's policy
and try metadata IPv4, ULA IPv6, native-looking IPv6 with namespace-local DNAT,
and an internally routed public-shaped address. Their child exits0 **and audit
exits1** are required. Firewall counters (including the DNAT rule) must record
the attempt and forbidden endpoints must receive no HTTP. A failed connect is
never evidence of zero attempts. Normal allowed HTTP/SNI/IPv6 positive controls
prove the workload really ran. This is proof for this namespace/rules/image;
future production routing/egress needs independent approval and evidence.

## PG18 linkage

Run `integration,executionsnapshot` serially against the existing guarded
disposable test database at schema6 or schema7, with pinned Goose3.26.0 on PATH. It must not
run concurrently with historical migration suites. Prepare a Linux test binary
in an absolute tool directory and provide fresh evidence paths:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go test -c -tags=executionnet \
  -o /absolute/tools/execution.test ./internal/monitoring
EXECUTION_TEST_TOOLS=/absolute/tools EXECUTION_PG_EVIDENCE=/absolute/fresh-pg-evidence \
  go test -tags=integration,executionsnapshot ./internal/monitoring \
  -run '^TestSystemSnapshot' -count=1 -v
```

Use the Docker engine's architecture (amd64 on hosted Linux, arm64 on this
development host), not a guessed value. TEST_DATABASE_URL is validated by
`internal/testdb`; missing PG18/URL/schema6-or7 is an error. Existing migrations are
applied explicitly by the operator/test job, never by the snapshot reader.

The controller is outside the network-none probe container. It reads the actual
PG18 snapshot with a separate restricted LOGIN role, serializes only fixture
configuration to the child's stdin pipe, and observes an HTTP receiver barrier.
While v7 HTTP is paused, real Owner conditional PATCH commits v8 and Session
revocation commits. Pool/pg_locks assertions show no probe-held DB resources.
The old fixture is delayed beyond v8's timeout, then released: the old result
must retain v7's timeout/status/version, and an independent v8 execution must
report its own version/status. No cross-process global quota is claimed; the
process-shared gate's cooling/constructor behavior is tested separately.

Report four evidence categories separately: static/unit/fuzz/race, actual Linux
DNS/HTTP/TLS, actual PG18, independent firewall/route/privilege proof. A successful
unit test or this document is not a substitute for any of the other categories.
