#!/bin/sh
set -eu
: "${PERIODIC91_TOOLS:?}"
test "$(go env GOVERSION)" = go1.24.3
git config --global --add safe.directory "$(pwd)"
CGO_ENABLED=0 go build -tags=privateworker -trimpath -o "$PERIODIC91_TOOLS/storagereporter" ./cmd/storagereporter
CGO_ENABLED=0 go build -tags=privateprobe -trimpath -o "$PERIODIC91_TOOLS/privatenetwork" ./cmd/privatenetwork
CGO_ENABLED=0 go build -tags=privateprobe -trimpath -o "$PERIODIC91_TOOLS/privateprobe" ./cmd/privateprobe
CGO_ENABLED=0 go build -tags=privateworker,privateprobe -trimpath -o "$PERIODIC91_TOOLS/privateapproval" ./cmd/privateapproval
tags=integration,schedulerfixture,privateworker,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateloopfixture,capacityfixture,privateservicefixture,approvedfixture
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/approved-pg.test" ./tests/integration
CGO_ENABLED=0 GOBIN="$PERIODIC91_TOOLS" go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
go test -race -tags=schedulerfixture,privateworker,privateprobe ./internal/scheduler ./internal/workerruntime ./internal/probenetwork ./internal/monitoring/targetpermit ./internal/supervisorruntime
go test ./internal/monitoring -count=1
go vet -tags=privateworker,privateprobe ./cmd/privateapproval ./cmd/privateworker ./cmd/privateprobe
