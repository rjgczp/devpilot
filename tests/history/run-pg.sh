#!/bin/sh
# Only a new disposable PG18 cluster; never accepts a caller database URL.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
export PATH="$repo/.devpilot-dashboard/bin:$PATH"
test "$(goose -env=none -version)" = 'goose version: v3.26.0'
project="devpilot-history-$$"
compose() { docker compose -p "$project" -f tests/history/compose.yaml "$@"; }
cleanup() { compose down -v >/dev/null 2>&1; }
trap cleanup EXIT
trap 'exit 130' INT TERM
for mode in normal race; do
  compose up -d --wait
  port=$(compose port postgres 5432)
  export DATABASE_URL= TEST_DATABASE_URL="postgres://devpilot_test:disposable_history_only@$port/devpilot_registration_test?sslmode=disable"
  if [ "$mode" = race ]; then
    go test -race -tags=integration,historyfixture ./tests/integration -run '^TestMonitorHistoryReadOnlyPG$' -count=1 -v
  else
    go test -tags=integration,historyfixture ./tests/integration -run '^TestMonitorHistoryReadOnlyPG$' -count=1 -v
  fi
  compose down -v
done
