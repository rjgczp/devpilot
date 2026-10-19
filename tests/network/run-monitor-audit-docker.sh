#!/bin/sh
# Host wrapper: creates and removes ONLY its own disposable PG18 container.
# Supports native Linux and macOS with a Linux Docker engine. Uses the exact
# local Go toolchain, cross-compiling pure-Go audit binaries when necessary.
set -eu
repo=$(git rev-parse --show-toplevel)
cd "$repo"
: "${MONITOR_AUDIT_OUTPUT:?absolute evidence directory required}"
case "$MONITOR_AUDIT_OUTPUT" in /*) ;; *) echo 'absolute output path required';exit 2;; esac
mkdir -p "$MONITOR_AUDIT_OUTPUT"
tools=$(mktemp -d)
cid=
cleanup(){
    [ -z "$cid" ] || docker rm -f "$cid" >/dev/null
    rm -rf "$tools"
}
trap cleanup EXIT INT TERM
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64;; x86_64|amd64) arch=amd64;; *) echo 'unsupported audit architecture';exit 2;; esac
GOTOOLCHAIN=local GOOS=linux GOARCH="$arch" CGO_ENABLED=0 \
    go test -c -tags=integration,networkaudit -o "$tools/integration.test" ./tests/integration
cache=$(go env GOMODCACHE)
GOTOOLCHAIN=local GOOS=linux GOARCH="$arch" CGO_ENABLED=0 GOBIN= \
    GOPATH="$tools/cross" GOMODCACHE="$cache" \
    go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
if [ -x "$tools/cross/bin/linux_$arch/goose" ]; then
    cp "$tools/cross/bin/linux_$arch/goose" "$tools/goose"
else
    cp "$tools/cross/bin/goose" "$tools/goose"
fi
python3 tests/network/check_audit_build.py "$tools/integration.test" "$tools/goose" > "$MONITOR_AUDIT_OUTPUT/builds.json"
docker build -t devpilot-monitor-audit -f tests/network/Dockerfile .
cid=$(docker run -d -e POSTGRES_USER=devpilot_test -e POSTGRES_PASSWORD=registration_test_only \
    -e POSTGRES_DB=devpilot_registration_test \
    postgres:18@sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336)
ready=0
for attempt in 1 2 3 4 5 6 7 8 9 10; do
    if docker exec "$cid" pg_isready -U devpilot_test -d devpilot_registration_test >/dev/null; then ready=1;break;fi
    sleep 1
done
[ "$ready" = 1 ] || exit 2
docker run --rm --cap-add NET_ADMIN --cap-add SYS_PTRACE --network "container:$cid" \
    -v "$repo:$repo:ro" -v "$tools:/toolbin:ro" -v "$MONITOR_AUDIT_OUTPUT:/evidence" -w "$repo" \
    -e PATH=/toolbin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
    -e MONITOR_PROBE_ISOLATED=1 -e MONITOR_TEST_BINARY=/toolbin/integration.test -e MONITOR_AUDIT_OUTPUT=/evidence \
    -e 'TEST_DATABASE_URL=postgres://devpilot_test:registration_test_only@127.0.0.1:5432/devpilot_registration_test?sslmode=disable' \
    devpilot-monitor-audit tests/network/run-monitor-audit.sh
