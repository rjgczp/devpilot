#!/bin/sh
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
action=${1:-status}
case "$action" in
 build)
  test "$#" = 1
  docker build -f "$repo/deploy/privateservice/Dockerfile" -t devpilot-private-runtime "$repo"
  docker image inspect devpilot-private-runtime --format '{{.Id}}'
  ;;
 init|start|run|stop|status)
  shift
  exec python3 -S "$repo/runtime/private/launcher.py" "$action" "$@"
  ;;
 *) exit 2;;
esac
