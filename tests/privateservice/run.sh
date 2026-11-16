#!/bin/sh
set -eu
test "$(uname -s)" = Linux
test "$#" = 1
case "$1" in /*) ;; *) exit 2;; esac
test ! -e "$1"
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
export PYTHONDONTWRITEBYTECODE=1
python3 tests/privateloop/build_candidate.py --check
python3 tests/capacity/build_candidate.py --check
python3 -m unittest discover -s tests/privateservice -v
mkdir -p "$1/tools"
python3 tests/scheduler/record.py --evidence "$1" --name runtime-build -- sh scripts/private-service.sh build
python3 tests/scheduler/record.py --evidence "$1" --name fixture-build -- docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
python3 tests/scheduler/record.py --evidence "$1" --name build -- docker run --rm \
 -v "$repo:$repo:ro" -v "$1/tools:/tools" -w "$repo" \
 -v devpilot-fixture-gomod:/go/pkg/mod -v devpilot-fixture-gobuild:/root/.cache/go-build \
 -e PERIODIC91_TOOLS=/tools -e GOTOOLCHAIN=local \
 golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh tests/privateservice/build.sh
python3 tests/scheduler/record.py --evidence "$1/receipt" --name lifecycle -- python3 tests/privateservice/lifecycle.py --tools "$1/tools" --evidence "$1/lifecycle"
