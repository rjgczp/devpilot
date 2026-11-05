#!/bin/sh
# Dedicated ephemeral demo, no user production env/database or ordinary stack.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo"
for tool in docker npm python3 openssl; do command -v "$tool" >/dev/null; done
python3 - <<'PY'
import socket
for family,host in [(socket.AF_INET,'127.0.0.1'),(socket.AF_INET6,'::1')]:
 with socket.socket(family) as s:
  s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
  try:s.bind((host,5174))
  except OSError:raise SystemExit('Demo loopback port5174 unavailable; stop its owner first.')
PY
umask 077
export PYTHONDONTWRITEBYTECODE=1
mkdir -p .devpilot-isolated-demo/tools
state="$repo/.devpilot-isolated-demo"
evidence=${DEVPILOT_DEMO_EVIDENCE:-"$state/$(date +%Y%m%d-%H%M%S)-$$"}
stop_file="$state/stop-$$"
credentials="$state/credentials-$$.json"
runner_pid=;ui_pid=
cleanup() {
 trap - EXIT INT TERM
 if [ -n "$runner_pid" ]; then kill -TERM "$runner_pid" 2>/dev/null || true; wait "$runner_pid" 2>/dev/null || true; fi
 if [ -n "$ui_pid" ]; then kill "$ui_pid" 2>/dev/null || true; wait "$ui_pid" 2>/dev/null || true; fi
 rm -f "$credentials" "$stop_file"
}
trap cleanup EXIT
trap 'exit 130' INT TERM
docker build -t devpilot-scheduler-fixture -f tests/scheduler/Dockerfile .
docker run --rm -v "$repo:$repo:ro" -v "$state/tools:/tools" -w "$repo" \
 -v devpilot-fixture-gomod:/go/pkg/mod -v devpilot-fixture-gobuild:/root/.cache/go-build \
 -e PERIODIC91_TOOLS=/tools -e GOTOOLCHAIN=local \
 golang:1.24.3-bookworm@sha256:29d97266c1d341b7424e2f5085440b74654ae0b61ecdba206bc12d6264844e21 sh tests/scheduler/build-periodic91.sh
npm ci --prefix web
DEVPILOT_ISOLATED_DEMO=1 node web/node_modules/vite/bin/vite.js web --config web/vite.config.ts --host 127.0.0.1 --strictPort > "$state/vite.log" 2>&1 &
ui_pid=$!
python3 tests/demo/demo.py --tools "$state/tools" --evidence "$evidence" --credentials "$credentials" --stop-file "$stop_file" &
runner_pid=$!
echo "本地隔离演示；仅固定fixture；临时登录信息见 $credentials"
echo "证据 ${evidence}；结束后自毁测试库。Ctrl-C停止；不将容器删除当作物理释放证明。"
wait "$runner_pid"
