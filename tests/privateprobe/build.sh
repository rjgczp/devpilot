#!/bin/sh
set -eu
: "${PERIODIC91_TOOLS:?absolute output directory}"
sh tests/scheduler/build-periodic91.sh
CGO_ENABLED=0 go build -tags=privateprobe -trimpath -o "$PERIODIC91_TOOLS/privateprobe" ./cmd/privateprobe
CGO_ENABLED=0 go build -tags=privateprobe -trimpath -o "$PERIODIC91_TOOLS/privatenetwork" ./cmd/privatenetwork
go test -tags=schedulerfixture ./internal/probenetwork -count=1
go test -race -tags=schedulerfixture ./internal/probenetwork -count=1
go test -tags=privateprobe ./internal/monitoring -run '^TestSupervisorLifetime' -count=1
go test -race -tags=privateprobe ./internal/monitoring -run '^TestSupervisorLifetime' -count=1
go test ./internal/monitoring -run 'TestFixtureSchedulerBuildBoundary|TestNetworkSupervisorFixtureIsAbsentFromRelease|TestDormantProductionDependencyAndSchemaBoundary|TestReleaseArtifactAndProbeTestBoundary' -count=1
go vet -tags=privateprobe ./cmd/privateprobe ./cmd/privatenetwork ./internal/probenetwork ./internal/monitoring/...
go version -m "$PERIODIC91_TOOLS/privateprobe" "$PERIODIC91_TOOLS/privatenetwork"
