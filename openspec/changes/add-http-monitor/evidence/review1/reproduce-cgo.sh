#!/bin/sh
# Diagnostic counterexample ONLY: deliberately CGO-enabled historical setup.
# The normal audit build gate rejects these tools; this run must fail auditing.
set -eu
repo=$(git rev-parse --show-toplevel)
: "${MONITOR_CGO_TOOLS:?absolute output/tools directory required}"
: "${MONITOR_CGO_EVIDENCE:?absolute evidence directory required}"
mkdir -p "$MONITOR_CGO_TOOLS" "$MONITOR_CGO_EVIDENCE"
# This exact Go version is independent of the production toolchain selection.
docker run --rm -v "$repo:/repo:ro" -v "$(go env GOMODCACHE):/go/pkg/mod" \
    -v "$MONITOR_CGO_TOOLS:/out" -w /repo -e GOTOOLCHAIN=local \
    golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh -eu -c '
    CGO_ENABLED=1 go test -c -tags=integration,networkaudit -o /out/integration.test ./tests/integration
    CGO_ENABLED=1 GOBIN=/out go install -tags="no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" github.com/pressly/goose/v3/cmd/goose@v3.26.0
    CGO_ENABLED=1 go build -o /out/pgx-cgo openspec/changes/add-http-monitor/evidence/review1/pgx-defaults-probe.go
    CGO_ENABLED=0 go build -o /out/pgx-pure openspec/changes/add-http-monitor/evidence/review1/pgx-defaults-probe.go
    cc -shared -fPIC -o /out/libgetpwuid-marker.so openspec/changes/add-http-monitor/evidence/review1/getpwuid-marker.c -ldl
    go version > /out/toolchain.txt
    go version -m /out/integration.test > /out/integration.build.txt
    go version -m /out/goose > /out/goose.build.txt
    '
cp "$MONITOR_CGO_TOOLS/"*.txt "$MONITOR_CGO_EVIDENCE/"
docker image inspect golang:1.24.3-bookworm --format '{{json .RepoDigests}}' > "$MONITOR_CGO_EVIDENCE/image.json"
# A parser-only probe needs no DB/network. A diagnostic interposer observes
# getpwuid_r and forwards it unchanged (normal acceptance never loads this shim).
for mode in cgo pure; do
    docker run --rm --network none --cap-add SYS_PTRACE -v "$MONITOR_CGO_TOOLS:/toolbin:ro" \
        -v "$MONITOR_CGO_EVIDENCE:/evidence" devpilot-monitor-audit -c \
        "strace -E LD_PRELOAD=/toolbin/libgetpwuid-marker.so -f -qq -yy -s 256 -e trace=process,connect,sendto,sendmsg,sendmmsg -o /evidence/pgx-$mode.trace /toolbin/pgx-$mode" \
        > "$MONITOR_CGO_EVIDENCE/pgx-$mode.txt" 2>&1
    set +e
    python3 tests/network/audit_trace.py "$MONITOR_CGO_EVIDENCE/pgx-$mode.trace" > "$MONITOR_CGO_EVIDENCE/pgx-$mode.audit.txt"
    status=$?
    set -e
    echo "child_exit=0 audit_exit=$status" > "$MONITOR_CGO_EVIDENCE/pgx-$mode.exit.txt"
    expected=0
    [ "$mode" = pure ] || expected=1
    [ "$status" = "$expected" ]
done
cid=$(docker run -d -e POSTGRES_USER=devpilot_test -e POSTGRES_PASSWORD=registration_test_only \
    -e POSTGRES_DB=devpilot_registration_test \
    postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336)
trap 'docker rm -f "$cid" >/dev/null' EXIT INT TERM
ready=0
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if docker exec "$cid" pg_isready -U devpilot_test -d devpilot_registration_test >/dev/null; then ready=1;break;fi
    sleep 1
done
[ "$ready" = 1 ]
set +e
docker run --rm --cap-add NET_ADMIN --cap-add SYS_PTRACE --network "container:$cid" \
    -v "$repo:/repo:ro" -v "$MONITOR_CGO_TOOLS:/toolbin:ro" -v "$MONITOR_CGO_EVIDENCE:/evidence" -w /repo \
    -e PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    -e MONITOR_PROBE_ISOLATED=1 -e MONITOR_TEST_BINARY=/toolbin/integration.test -e MONITOR_AUDIT_OUTPUT=/evidence \
    -e 'TEST_DATABASE_URL=postgres://devpilot_test:registration_test_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable' \
    devpilot-monitor-audit tests/network/run-monitor-audit.sh > "$MONITOR_CGO_EVIDENCE/runner.txt" 2>&1
status=$?
set -e
echo "expected_audit_failure=1 actual_exit=$status" > "$MONITOR_CGO_EVIDENCE/runner.exit.txt"
[ "$status" = 1 ]
grep -q 'child_exit=0 audit_exit=1' "$MONITOR_CGO_EVIDENCE/real.exit.txt"
grep -q 'approved_attempts=4 blocked_attempts=6' "$MONITOR_CGO_EVIDENCE/real.audit.txt"
# Even the preflight must reject these actual CGO tools in either normal role.
set +e
python3 tests/network/check_audit_build.py "$MONITOR_CGO_TOOLS/integration.test" "$MONITOR_CGO_TOOLS/goose" \
    > "$MONITOR_CGO_EVIDENCE/build-gate.txt" 2>&1
status=$?
set -e
echo "expected_build_rejection=1 actual_exit=$status" > "$MONITOR_CGO_EVIDENCE/build-gate.exit.txt"
[ "$status" = 1 ]
