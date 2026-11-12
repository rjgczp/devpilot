#!/bin/sh
# Explicit offline tooling only. Never runs on application startup.
set -eu
export PGHOST=postgres PGPORT=5432 PGUSER=devpilot_migrator PGDATABASE=devpilot_private
PGPASSWORD=$(cat /run/secrets/migration_password)
case "$PGPASSWORD" in ''|*[!0-9a-f]*) echo 'Invalid migrator secret' >&2; exit 1;; esac
test "${#PGPASSWORD}" = 64
export PGPASSWORD
test "$(psql -XAt -v ON_ERROR_STOP=1 -c "SELECT current_database()='devpilot_private' AND current_user='devpilot_migrator' AND current_setting('server_version_num')::integer/10000=18")" = t
closed() {
  test "$(psql -XAt -v ON_ERROR_STOP=1 -c "SELECT count(*)=1 AND bool_and(gate='closed' AND emergency_stop) FROM public.scheduler_control WHERE control_id='deployment'")" = t
}
case "${1:-check}" in
  migrate)
    # Refuse to upgrade an existing open deployment, never close it implicitly.
    exists=$(psql -XAt -v ON_ERROR_STOP=1 -c "SELECT to_regclass('public.scheduler_control') IS NOT NULL")
    if [ "$exists" = t ]; then closed; fi
    export GOOSE_DRIVER=postgres
    export GOOSE_DBSTRING="postgres://$PGUSER:$PGPASSWORD@$PGHOST:$PGPORT/$PGDATABASE?sslmode=disable"
    test "$(goose -env=none -version)" = 'goose version: v3.26.0'
    goose -env=none -dir /migrations up
    closed
    ;;
  provision)
    closed
    app_password=$(cat /run/secrets/app_password)
    case "$app_password" in ''|*[!0-9a-f]*) echo 'Invalid application secret' >&2; exit 1;; esac
    test "${#app_password}" = 64
    # Hex-only value, private stdin; no password in argv, log, or source.
    { printf "\\set app_password '%s'\n" "$app_password"; cat /provision.sql; } | psql -X -v ON_ERROR_STOP=1 -1
    ;;
  check) closed ;;
  *) echo 'Usage: migrate | provision | check' >&2; exit 2;;
esac
