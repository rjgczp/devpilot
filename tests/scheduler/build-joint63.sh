#!/bin/sh
set -eu
: "${JOINT63_TOOLS:?absolute fixture tools directory required}"
RELEASE54_TOOLS="$JOINT63_TOOLS" sh tests/scheduler/build-release54.sh
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture ./internal/monitoring/... ./cmd/schedulerprobefixture ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go build -tags=schedulerfixture -o "$JOINT63_TOOLS/schedulerprobefixture" ./cmd/schedulerprobefixture
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture -o "$JOINT63_TOOLS/joint-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture,releasefixture,jointfixture -o "$JOINT63_TOOLS/joint-pg-race.test" ./tests/integration

BUDGET_TOOLS="$JOINT63_TOOLS" sh tests/scheduler/build-budget.sh
