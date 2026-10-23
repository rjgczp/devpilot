#!/bin/sh
# Native Linux builder only. Build-time dependencies precede isolated tests.
set -eu
test "$(uname -s)" = Linux
: "${NETWORK64_TOOLS:?dedicated absolute tools directory required}"
case "$NETWORK64_TOOLS" in /*) ;; *) exit 2;; esac
mkdir -p "$NETWORK64_TOOLS"
GOTOOLCHAIN=local go vet -tags=integration,schedulerfixture,budgetfixture,networkfixture ./internal/supervisorfixture ./cmd/networksupervisorfixture ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=0 go build -tags=schedulerfixture -o "$NETWORK64_TOOLS/networksupervisorfixture" ./cmd/networksupervisorfixture
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=schedulerfixture -o "$NETWORK64_TOOLS/supervisorfixture.test" ./internal/supervisorfixture
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=schedulerfixture -o "$NETWORK64_TOOLS/supervisorfixture-race.test" ./internal/supervisorfixture
GOTOOLCHAIN=local CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,budgetfixture,networkfixture -o "$NETWORK64_TOOLS/network64-pg.test" ./tests/integration
GOTOOLCHAIN=local CGO_ENABLED=1 go test -c -race -tags=integration,schedulerfixture,budgetfixture,networkfixture -o "$NETWORK64_TOOLS/network64-pg-race.test" ./tests/integration
cp "$(command -v goose)" "$NETWORK64_TOOLS/goose"
