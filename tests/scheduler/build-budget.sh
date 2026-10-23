#!/bin/sh
# Separate database-only fixtures; never merge terminationfixture into jointfixture.
set -eu
test "$(uname -s)" = Linux
: "${BUDGET_TOOLS:?absolute fixture tools directory required}"
case "$BUDGET_TOOLS" in /*) ;; *) exit 2;; esac
mkdir -p "$BUDGET_TOOLS"
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture -o "$BUDGET_TOOLS/budget-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture -o "$BUDGET_TOOLS/budget-pg-race.test" ./tests/integration
cp "$(command -v goose)" "$BUDGET_TOOLS/goose"
