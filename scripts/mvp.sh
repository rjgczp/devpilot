#!/bin/sh
# One local entry; existing scripts retain all DB/network/physical boundaries.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
usage() {
  echo 'Usage: ./scripts/mvp.sh [manage|demo]'
  echo '  manage (default): persistent local management, http://127.0.0.1:5173, execution CLOSED'
  echo '  demo: disposable fixed-fixture demo, http://127.0.0.1:5174, explicit Owner consent, max3 real60s cycles'
  echo 'Use separate browser profiles/incognito sessions when logging in to both modes (Cookies are not port-scoped).'
  echo 'Details: docs/mvp.md'
}
if [ "$#" -gt 1 ]; then usage >&2; exit 2; fi
case "${1:-manage}" in
  manage)
    echo 'MVP: 普通管理模式；持久化本地库；执行保持关闭。'
    export DEVPILOT_ISOLATED_DEMO=0
    exec "$repo/scripts/dev-dashboard.sh"
    ;;
  demo)
    echo 'MVP: 隔离演示模式；仅固定fixture；临时Owner必须在Web明确授权。'
    echo '并行登录普通模式时请使用独立浏览器配置/无痕会话，避免Cookie覆盖。'
    exec "$repo/scripts/dev-isolated-demo.sh"
    ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
