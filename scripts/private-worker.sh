#!/bin/sh
# Native Linux, existing internal control network, no privilege provisioning.
set -eu
test "$(uname -s)" = Linux
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "${1:-check}" in
  build) exec docker build -t devpilot-privateworker:local -f "$repo/deploy/privateworker/Dockerfile" "$repo" ;;
  run|check) ;;
  *) echo 'Usage: sh scripts/private-worker.sh [build|check|run]' >&2; exit 2;;
esac
: "${WORKER_CONTROL_NETWORK:?existing internal PG-only network}"
: "${WORKER_DATABASE_URL_FILE:?restricted Worker URL secret file}"
: "${WORKER_INCARNATION:?offline assigned UUID}"
test "$(docker network inspect "$WORKER_CONTROL_NETWORK" --format '{{.Internal}}')" = true
case "$WORKER_DATABASE_URL_FILE" in /*) ;; *) echo 'secret filename must be absolute' >&2; exit 2;; esac
test -f "$WORKER_DATABASE_URL_FILE"
docker image inspect devpilot-privateworker:local >/dev/null
if [ "${1:-check}" = check ]; then
  exec docker compose -f "$repo/deploy/privateworker/compose.closed.yaml" run --rm --no-deps --pull never worker \
    --database-url-file /run/secrets/worker_url --incarnation "$WORKER_INCARNATION" --check
fi
exec docker compose -f "$repo/deploy/privateworker/compose.closed.yaml" run --rm --no-deps --pull never worker
