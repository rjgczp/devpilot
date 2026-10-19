#!/bin/sh
# Supply the guarded disposable TEST_DATABASE_URL and native pinned Goose PATH.
set -eu
cd "$(git rev-parse --show-toplevel)"
: "${TEST_DATABASE_URL:?dedicated PG18 test database required}"
: "${MONITOR_REVIEW_OUTPUT:?absolute output directory required}"
mkdir -p "$MONITOR_REVIEW_OUTPUT"
run() {
    name=$1; shift
    echo "$*" > "$MONITOR_REVIEW_OUTPUT/$name.command"
    set +e
    "$@" > "$MONITOR_REVIEW_OUTPUT/$name.txt" 2>&1
    status=$?
    set -e
    echo "exit=$status" | tee "$MONITOR_REVIEW_OUTPUT/$name.exit.txt"
    [ "$status" = 0 ] || { cat "$MONITOR_REVIEW_OUTPUT/$name.txt"; exit "$status"; }
}
run python python3 -m unittest discover -s tests/network -p 'test_*.py' -v
run vet go vet ./...
run vet-integration go vet -tags=integration ./...
run unit go test ./... -count=1
run race go test -race ./... -count=1
run integration go test -tags=integration ./... -count=1 -timeout=120s
run integration-race go test -race -tags=integration ./tests/integration -count=1 -timeout=120s
run openspec-change openspec validate add-http-monitor --strict --json
run diff-check git diff --check
run format gofmt -l cmd internal tests
[ ! -s "$MONITOR_REVIEW_OUTPUT/format.txt" ]
