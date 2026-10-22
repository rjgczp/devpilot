#!/bin/sh
set -eu
: "${RELEASE54_TOOLS:?absolute fixture tool directory required}"
STOP65_TOOLS="$RELEASE54_TOOLS" sh tests/scheduler/build-stop65.sh
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture ./internal/supervisorfixture ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture -o "$RELEASE54_TOOLS/release-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture -o "$RELEASE54_TOOLS/release-pg-race.test" ./tests/integration
