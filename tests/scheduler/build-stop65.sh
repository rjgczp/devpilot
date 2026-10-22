#!/bin/sh
# Adds no production entry or database grant. Original6.4 binaries also rebuilt.
set -eu
: "${STOP65_TOOLS:?dedicated absolute tools directory required}"
NETWORK64_TOOLS="$STOP65_TOOLS" sh tests/scheduler/build-network64.sh
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture,terminationfixture ./internal/supervisorfixture ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture,terminationfixture -o "$STOP65_TOOLS/physical-stop-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture,terminationfixture -o "$STOP65_TOOLS/physical-stop-pg-race.test" ./tests/integration
