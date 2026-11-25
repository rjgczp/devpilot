#!/bin/sh
set -eu
test "$(uname -s)" = Linux
test "$#" = 1
case "$1" in /*) ;; *) exit 2;; esac
test ! -e "$1"
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
export PYTHONDONTWRITEBYTECODE=1
mkdir -p "$1/tools"
# Secret-bearing harness files are never inside uploaded evidence.
private_lite_tmp=$(mktemp -d)
chmod 700 "$private_lite_tmp"
export TMPDIR="$private_lite_tmp"
trap 'rm -rf "$private_lite_tmp"' EXIT
python3 -m unittest discover -s tests/private_lite -v
python3 -m unittest discover -s tests/privateservice -v
python3 -m unittest discover -s tests/release -v
python3 -S runtime/private_lite/launcher.py run --state "$1/closed"
python3 tests/scheduler/record.py --evidence "$1/build" --name legacy-runtime -- sh scripts/private-service.sh build
python3 tests/scheduler/record.py --evidence "$1/build" --name lite-runtime -- docker build -t devpilot-private-lite-runtime -f deploy/private-lite/Dockerfile .
python3 tests/scheduler/record.py --evidence "$1/build" --name fixture -- docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
python3 tests/scheduler/record.py --evidence "$1/build" --name tools -- docker run --rm \
 -v "$repo:$repo:ro" -w "$repo" -v "$1/tools:/tools" \
 -v devpilot-fixture-gomod:/go/pkg/mod -v devpilot-fixture-gobuild:/root/.cache/go-build \
 -e GOTOOLCHAIN=local golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 \
 sh -ec 'CGO_ENABLED=0 go test -c -tags=integration,schedulerfixture,privateworker,budgetfixture,networkfixture,releasefixture,jointfixture,resultfixture,periodicfixture,privateloopfixture,capacityfixture,privateservicefixture,approvedfixture -o /tools/approved-pg.test ./tests/integration
 CGO_ENABLED=0 go build -tags=privateworker -o /tools/storagereporter ./cmd/storagereporter
 CGO_ENABLED=0 GOBIN=/tools go install -tags="no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb" github.com/pressly/goose/v3/cmd/goose@v3.26.0
 go test -race -tags=privateprobe,privateworker ./internal/monitoring ./internal/probenetwork ./internal/supervisorruntime ./internal/monitoring/targetpermit'
python3 - "$1/tools" <<'PY'
import subprocess,sys
cid=subprocess.check_output(['docker','create','devpilot-private-lite-runtime'],text=True).strip()
try:
 for tool in ['privateprobe','privatenetwork','privateapproval','liteprobe','litenetwork','liteapproval']:
  subprocess.run(['docker','cp',cid+':/toolbin/'+tool,sys.argv[1]+'/'+tool],check=True)
finally:subprocess.run(['docker','rm',cid],check=True)
PY
python3 tests/scheduler/record.py --evidence "$1/receipts" --name packets -- python3 tests/private_lite/network.py "$1/packets"
for scenario in allow unapproved dns-change private4 private6; do
 python3 tests/scheduler/record.py --evidence "$1/receipts" --name "$scenario" -- python3 tests/private_lite/lifecycle.py --tools "$1/tools" --evidence "$1/$scenario" --case "$scenario"
done
