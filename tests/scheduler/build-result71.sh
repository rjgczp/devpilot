#!/bin/sh
# Separate tagged result binary; never part of a normal release build.
set -eu
: "${RESULT71_TOOLS:?absolute fixture tools directory required}"
JOINT63_TOOLS="$RESULT71_TOOLS" sh tests/scheduler/build-joint63.sh
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture ./internal/scheduler ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture -o "$RESULT71_TOOLS/result-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture -o "$RESULT71_TOOLS/result-pg-race.test" ./tests/integration
GOTOOLCHAIN=local go test -tags=schedulerfixture ./internal/scheduler -count=1
GOTOOLCHAIN=local go test -race -tags=schedulerfixture ./internal/scheduler -count=1
