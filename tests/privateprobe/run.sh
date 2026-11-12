#!/bin/sh
set -eu
test "$#" = 1
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
 golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh tests/privateprobe/build.sh
for mode in normal race; do
 for scenario in allow-https permit-missing supervisor-lost lifetime-lost rebinding ipv6-poison tls-invalid; do
  python3 tests/scheduler/record.py --evidence "$1/receipts" --name "$mode-$scenario" -- \
   python3 tests/privateprobe/lifecycle.py --tools "$1/tools" --evidence "$1/$mode-$scenario" --mode "$mode" --case "$scenario"
 done
done
python3 tests/privateprobe/lifecycle.py --tools "$1/tools" --evidence "$1/untrusted-stop" --mode normal --case supervisor-killed
python3 tests/privateprobe/expiry.py --tools "$1/tools" --evidence "$1/kernel-expiry"
# Focused original fixture regression, no historical full matrix.
python3 tests/scheduler/joint_fixture.py --evidence "$1/original-fixture" --supervisor "$1/tools/networksupervisorfixture" --probe "$1/tools/schedulerprobefixture" --pg-binary "$1/tools/periodic-pg.test" --mode normal --case allow-https
