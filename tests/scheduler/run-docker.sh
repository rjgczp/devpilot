#!/bin/sh
# Repeatable isolated Task9.1 acceptance, never a production launcher.
set -eu
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 /absolute/new-evidence-directory [normal|race|both]" >&2
  exit 2
fi
evidence=$1
mode=${2:-both}
case "$evidence" in /*) ;; *) echo 'evidence must be absolute' >&2; exit 2;; esac
case "$mode" in normal|race|both) ;; *) exit 2;; esac
test ! -e "$evidence"
repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cd "$repo"
mkdir -p "$evidence/tools"
export PYTHONDONTWRITEBYTECODE=1
python3 tests/scheduler/record.py --evidence "$evidence" --name image-build -- docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
# Preparation may download pinned build dependencies. Acceptance containers
# below use only a private internal control bridge and network-none data plane.
python3 tests/scheduler/record.py --evidence "$evidence" --name build -- docker run --rm \
  -v "$repo:$repo:ro" -v "$evidence/tools:/tools" -w "$repo" \
  -v devpilot-fixture-gomod:/go/pkg/mod -v devpilot-fixture-gobuild:/root/.cache/go-build \
  -e PERIODIC91_TOOLS=/tools -e GOTOOLCHAIN=local \
  golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh tests/scheduler/build-periodic91.sh
python3 tests/scheduler/preflight-docker.py --evidence "$evidence/preflight"
for selected in normal race; do
  if [ "$mode" = both ] || [ "$mode" = "$selected" ]; then
    python3 tests/scheduler/record.py --evidence "$evidence" --name "periodic-$selected" \
      --binary "$evidence/tools/periodic-pg.test" --binary "$evidence/tools/periodic-pg-race.test" \
      --binary "$evidence/tools/networksupervisorfixture" --binary "$evidence/tools/schedulerprobefixture" \
      -- python3 tests/scheduler/periodic91.py --evidence "$evidence/$selected" --tools "$evidence/tools" --mode "$selected"
    binary=periodic-pg.test
    if [ "$selected" = race ]; then binary=periodic-pg-race.test; fi
    # The only production-function change is completion scheduling. Reuse the
    # existing ACK/idempotence and old-result Pause/PATCH regressions, not the
    # historical full network/fault matrices.
    for regression in TestResultCommitACKPG TestResultCommitPausePG TestResultCommitPatchPG; do
      python3 tests/scheduler/record.py --evidence "$evidence" --name "$selected-$regression" \
        --binary "$evidence/tools/$binary" -- python3 tests/scheduler/result_fixture.py \
        --evidence "$evidence/$selected-$regression" --supervisor "$evidence/tools/networksupervisorfixture" \
        --probe "$evidence/tools/schedulerprobefixture" --pg-binary "$evidence/tools/$binary" \
        --mode "$selected" --case allow-https --test "$regression"
    done
  fi
done
echo "Task9.1 evidence: $evidence (production closed; Task8.3 deferred)"
