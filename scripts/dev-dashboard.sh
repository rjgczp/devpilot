#!/bin/sh
# One local UI/API/PG workflow. No scheduler gate or target networking is enabled.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
for tool in docker go npm python3 openssl; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
python3 - <<'PY'
import socket
for family,host in [(socket.AF_INET,'127.0.0.1'),(socket.AF_INET6,'::1')]:
 with socket.socket(family) as s:
  try:s.bind((host,5173))
  except OSError:raise SystemExit(f'Loopback {host}:5173 is unavailable. Stop its owner or enable local IPv6 before starting.')
PY
mkdir -p .devpilot-dashboard/bin
if [ ! -f .devpilot-dashboard.env ]; then
  (umask 077; printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 24)" > .devpilot-dashboard.env)
fi
# Dedicated local configuration, never the user's production .env/DATABASE_URL.
. ./.devpilot-dashboard.env
export POSTGRES_PASSWORD
compose() { docker compose --env-file .devpilot-dashboard.env -p devpilot-dashboard -f compose.dashboard.yaml "$@"; }
api_pid=
ui_pid=
cleanup() {
  trap - EXIT INT TERM
  if [ -n "$ui_pid" ]; then kill "$ui_pid" 2>/dev/null || true; wait "$ui_pid" 2>/dev/null || true; fi
  if [ -n "$api_pid" ]; then kill "$api_pid" 2>/dev/null || true; wait "$api_pid" 2>/dev/null || true; fi
  compose stop postgres >/dev/null 2>&1 || true
  echo 'Dashboard stopped. The dedicated local database volume is retained.'
}
trap cleanup EXIT
trap 'exit 0' INT TERM
compose up -d --wait postgres
cluster=$(compose exec -T postgres psql -U devpilot_dashboard -d devpilot_dashboard -At -c 'SHOW cluster_name')
test "$cluster" = devpilot-dashboard-local
export DATABASE_URL="postgres://devpilot_dashboard:$POSTGRES_PASSWORD@127.0.0.1:55432/devpilot_dashboard?sslmode=disable"
export AUTH_MODE=development AUTH_ORIGIN=http://127.0.0.1:5173 HTTP_ADDR='[::1]:5173'
export SESSION_TTL=24h GOTOOLCHAIN=local
if [ ! -x .devpilot-dashboard/bin/goose ]; then
  GOBIN="$repo/.devpilot-dashboard/bin" go install -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' github.com/pressly/goose/v3/cmd/goose@v3.26.0
fi
test "$(.devpilot-dashboard/bin/goose -env=none -version)" = 'goose version: v3.26.0'
GOOSE_DRIVER=postgres GOOSE_DBSTRING="$DATABASE_URL" .devpilot-dashboard/bin/goose -env=none -dir migrations up
npm ci --prefix web
go build -o .devpilot-dashboard/bin/api ./cmd/api
.devpilot-dashboard/bin/api > .devpilot-dashboard/api.log 2>&1 &
api_pid=$!
python3 - <<'PY'
import time,urllib.request
opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
for _ in range(50):
 try:
  with opener.open('http://[::1]:5173/readyz',timeout=1) as response:
   if response.status==200:break
 except OSError:pass
 time.sleep(.2)
else:raise SystemExit('API unavailable; inspect .devpilot-dashboard/api.log')
PY
printf '\nDevPilot Dashboard: http://127.0.0.1:5173\nRegister your own account, create a Workspace/Project, then add a Monitor.\nProduction execution remains closed. Ctrl-C stops this local stack.\n\n'
# Direct Node invocation gives the trap the actual server PID, not an npm parent.
node web/node_modules/vite/bin/vite.js web --config web/vite.config.ts --host 127.0.0.1 &
ui_pid=$!
wait "$ui_pid"
