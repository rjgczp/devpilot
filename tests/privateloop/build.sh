#!/bin/sh
set -eu
: "${PERIODIC91_TOOLS:?absolute output directory}"
sh tests/privateprobe/build.sh
CGO_ENABLED=0 go build -tags=privateworker -trimpath -o "$PERIODIC91_TOOLS/privateworker" ./cmd/privateworker
tags=integration,schedulerfixture,privateworker,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateloopfixture
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/private-loop-pg.test" ./tests/integration
go test -tags=privateworker ./internal/scheduler ./internal/workerruntime -count=1
go test -race -tags=privateworker ./internal/scheduler ./internal/workerruntime -count=1
go vet -tags=privateworker ./cmd/privateworker ./internal/workerruntime ./internal/scheduler
go version -m "$PERIODIC91_TOOLS/privateworker" "$PERIODIC91_TOOLS/private-loop-pg.test"
