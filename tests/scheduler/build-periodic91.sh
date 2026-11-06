#!/bin/sh
# Fixture-only; build exactly the affected runner and original trusted tools.
set -eu
test "$(uname -s)" = Linux
: "${PERIODIC91_TOOLS:?absolute output directory required}"
case "$PERIODIC91_TOOLS" in /*) ;; *) exit 2;; esac
test "$(go env GOVERSION)" = go1.24.3
# Read-only host checkouts can belong to the runner UID rather than container
# root. Trust only this explicitly mounted repository; retain VCS stamping.
git config --global --add safe.directory "$(pwd)"
mkdir -p "$PERIODIC91_TOOLS"
export GOTOOLCHAIN=local
CGO_ENABLED=0 go build -tags=schedulerfixture -o "$PERIODIC91_TOOLS/networksupervisorfixture" ./cmd/networksupervisorfixture
CGO_ENABLED=0 go build -tags=schedulerfixture -o "$PERIODIC91_TOOLS/schedulerprobefixture" ./cmd/schedulerprobefixture
tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture
go vet -tags="$tags" ./internal/scheduler ./internal/supervisorfixture ./tests/integration
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/periodic-pg.test" ./tests/integration
CGO_ENABLED=1 go test -c -race -tags="$tags" -o "$PERIODIC91_TOOLS/periodic-pg-race.test" ./tests/integration
CGO_ENABLED=0 GOBIN="$PERIODIC91_TOOLS" go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
go test -tags=schedulerfixture ./internal/scheduler ./internal/supervisorfixture -count=1
go test -race -tags=schedulerfixture ./internal/scheduler ./internal/supervisorfixture -count=1
go version -m "$PERIODIC91_TOOLS/periodic-pg.test" "$PERIODIC91_TOOLS/periodic-pg-race.test" "$PERIODIC91_TOOLS/networksupervisorfixture" "$PERIODIC91_TOOLS/schedulerprobefixture" "$PERIODIC91_TOOLS/goose"
