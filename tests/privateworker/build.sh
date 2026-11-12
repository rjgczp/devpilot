#!/bin/sh
set -eu
test "$(uname -s)" = Linux
: "${PERIODIC91_TOOLS:?absolute output directory required}"
sh tests/scheduler/build-periodic91.sh
CGO_ENABLED=0 go build -tags=privateworker -trimpath -o "$PERIODIC91_TOOLS/privateworker" ./cmd/privateworker
go version -m "$PERIODIC91_TOOLS/privateworker"
tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateworkerfixture
CGO_ENABLED=0 go test -c -tags="$tags" -o "$PERIODIC91_TOOLS/private-worker-pg.test" ./tests/integration
go test -tags=privateworker ./internal/scheduler ./internal/workerruntime -count=1
go test -race -tags=privateworker ./internal/scheduler ./internal/workerruntime -count=1
go vet -tags=privateworker ./cmd/privateworker ./internal/scheduler ./internal/workerruntime
go test ./internal/monitoring -run 'TestDormantProductionDependencyAndSchemaBoundary|TestFixtureSchedulerBuildBoundary|TestReleaseArtifactAndProbeTestBoundary' -count=1
if go list -tags=privateworker -deps ./cmd/api | grep -Eq '/internal/(scheduler|workerruntime|monitoring)(/|$)'; then
  echo 'API acquired an execution dependency' >&2; exit 1
fi
