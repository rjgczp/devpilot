#!/bin/sh
# Build phase may retrieve dependencies; runtime is the existing internal fixture.
set -eu
test "$#" -ge 1 && test "$#" -le 2
case "${2:-both}" in private|both) ;; *) exit 2;; esac
case "$1" in /*) ;; *) exit 2;; esac
test ! -e "$1"
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
mkdir -p "$1/tools"
export PYTHONDONTWRITEBYTECODE=1
python3 tests/scheduler/record.py --evidence "$1" --name image-build -- docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
python3 tests/scheduler/record.py --evidence "$1" --name build -- docker run --rm \
  -v "$repo:$repo:ro" -v "$1/tools:/tools" -w "$repo" \
  -v devpilot-fixture-gomod:/go/pkg/mod -v devpilot-fixture-gobuild:/root/.cache/go-build \
  -e PERIODIC91_TOOLS=/tools -e GOTOOLCHAIN=local \
  golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh tests/privateworker/build.sh
python3 tests/privateworker/lifecycle.py --tools "$1/tools" --evidence "$1/private"
# Exercise the unchanged fixture entry with the shared Worker, normal only.
if [ "${2:-both}" = both ]; then
  python3 tests/scheduler/periodic91.py --tools "$1/tools" --evidence "$1/original" --mode normal
fi
