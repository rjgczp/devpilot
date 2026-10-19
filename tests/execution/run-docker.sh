#!/bin/sh
set -eu
repo=$(git rev-parse --show-toplevel)
cd "$repo"
: "${EXECUTION_EVIDENCE:?absolute dedicated evidence directory required}"
case "$EXECUTION_EVIDENCE" in /*) ;; *) exit 2;; esac
mkdir -p "$EXECUTION_EVIDENCE"
tools=$(mktemp -d)
chmod 755 "$tools"
trap 'rm -rf "$tools"' EXIT INT TERM
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) arch=arm64;;x86_64|amd64) arch=amd64;;*) exit 2;;esac
GOTOOLCHAIN=local CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go test -c -tags=executionnet -o "$tools/execution.test" ./internal/monitoring
go version -m "$tools/execution.test" > "$EXECUTION_EVIDENCE/build.txt"
python3 - "$tools/execution.test" "$EXECUTION_EVIDENCE" <<'PY'
import hashlib,json,pathlib,sys
binary=pathlib.Path(sys.argv[1])
pathlib.Path(sys.argv[2],'binary.json').write_text(json.dumps({'sha256':hashlib.sha256(binary.read_bytes()).hexdigest()},indent=2)+'\n')
PY
python3 - "$EXECUTION_EVIDENCE/build.txt" <<'PY'
import pathlib,sys
b=pathlib.Path(sys.argv[1]).read_text()
assert 'CGO_ENABLED=0' in b and 'GOOS=linux' in b and 'go1.24.3' in b
PY
docker build -t devpilot-execution-audit -f tests/execution/Dockerfile .
docker image inspect devpilot-execution-audit --format '{{.Id}}' > "$EXECUTION_EVIDENCE/image-id.txt"
docker run --rm --network none --cap-add NET_ADMIN --cap-add SYS_PTRACE \
    --sysctl net.ipv6.conf.all.disable_ipv6=0 --memory=128m --cpus=1 --pids-limit=128 --ulimit nofile=256:256 \
    -v "$tools:/toolbin:ro" -v "$repo/tests/execution:/runner:ro" -v "$EXECUTION_EVIDENCE:/evidence" \
    -e EXECUTION_ISOLATED=1 devpilot-execution-audit -c 'python3 /runner/run.py'
