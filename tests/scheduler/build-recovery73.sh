#!/bin/sh
# Distinct legal fixture binary, excluded from ordinary release builds.
set -eu
: "${RESULT71_TOOLS:?absolute fixture tools directory required}"
sh tests/scheduler/build-result71.sh
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,recoveryfixture ./internal/scheduler ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,recoveryfixture -o "$RESULT71_TOOLS/recovery-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,recoveryfixture -o "$RESULT71_TOOLS/recovery-pg-race.test" ./tests/integration
