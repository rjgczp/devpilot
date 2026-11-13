#!/bin/sh
set -eu
: "${PERIODIC91_TOOLS:?absolute output directory}"
# Reuse the accepted Probe/network/physical tooling, not a new executor.
sh tests/privateprobe/build.sh
for entry in privateworker storagereporter retentioncontroller; do
 CGO_ENABLED=0 go build -tags=privateworker -trimpath -o "$PERIODIC91_TOOLS/$entry" "./cmd/$entry"
done
tags=integration,schedulerfixture,privateworker,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateloopfixture,capacityfixture
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/private-loop-pg.test" ./tests/integration
go test -tags=privateworker ./internal/storagecontrol ./internal/retentioncontrol ./internal/workerruntime ./internal/scheduler -count=1
go test -race -tags=privateworker ./internal/storagecontrol ./internal/retentioncontrol ./internal/workerruntime ./internal/scheduler -count=1
go vet -tags=privateworker ./cmd/storagereporter ./cmd/retentioncontroller ./cmd/privateworker ./internal/storagecontrol ./internal/retentioncontrol ./internal/workerruntime
go version -m "$PERIODIC91_TOOLS/storagereporter" "$PERIODIC91_TOOLS/retentioncontroller" "$PERIODIC91_TOOLS/privateworker" "$PERIODIC91_TOOLS/private-loop-pg.test"
