#!/bin/sh
# Inspection only. No deploy/apply/start/permission or network-opening verb.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
exec python3 -B -S "$repo/deploy/release/preflight.py" "$@"
