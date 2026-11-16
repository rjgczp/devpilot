#!/bin/sh
set -eu
: "${PERIODIC91_TOOLS:?absolute tools directory}"
test "$(go env GOVERSION)" = go1.24.3
git config --global --add safe.directory "$(pwd)"
mkdir -p "$PERIODIC91_TOOLS"
CGO_ENABLED=0 go build -tags=privateworker -trimpath -o "$PERIODIC91_TOOLS/storagereporter" ./cmd/storagereporter
tags=integration,schedulerfixture,privateworker,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateloopfixture,capacityfixture,privateservicefixture
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/private-service-pg.test" ./tests/integration
CGO_ENABLED=0 GOBIN="$PERIODIC91_TOOLS" go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
go test -race -tags=privateworker,privateprobe ./internal/supervisorruntime ./cmd/privatesupervisor
go test ./internal/monitoring -run '^TestPrivateSupervisorRequiresBothExplicitLinuxTags$' -count=1
go vet -tags=privateworker,privateprobe ./internal/supervisorruntime ./cmd/privatesupervisor
