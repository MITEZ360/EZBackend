#!/usr/bin/env bash
# 一键回归：启动干净的服务实例 + mock OAuth provider，按序跑全部 Python 接口回归脚本。
# 依赖：go、pnpm（仅首次构建前端产物可复用）、python3。无需外部数据库。
# 用法：bash test/regression/run_all.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

WORK="$(mktemp -d /tmp/tenon-regression-XXXXXX)"
BIN="$WORK/tenon"
PIDS=""

log() { echo "[regression] $*"; }

cleanup() {
  log "清理进程与临时文件"
  for pid in $PIDS; do kill "$pid" 2>/dev/null || true; done
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM

log "编译二进制"
GOTOOLCHAIN=local go build -o "$BIN" ./cmd/tenon

# 主服务配置（端口 9000，独立数据与上传目录，避免污染开发库）
MAIN_CFG="$WORK/config-main.yaml"
sed -e 's#dsn: data/tenon.db#dsn: '"$WORK"'/main.db#' \
    -e 's#dir: data/backups#dir: '"$WORK"'/backups#' \
    -e 's#local_dir: data/uploads#local_dir: '"$WORK"'/uploads#' \
    configs/config.yaml > "$MAIN_CFG"

# OAuth 演示配置（端口 9002，指向 mock provider 9101）
OAUTH_CFG="$WORK/config-oauth.yaml"
sed -e 's#dsn: data/tenon-oauth-demo.db#dsn: '"$WORK"'/oauth.db#' \
    -e 's#dir: data/backups-oauth-demo#dir: '"$WORK"'/backups-oauth#' \
    -e 's#local_dir: data/uploads-oauth-demo#local_dir: '"$WORK"'/uploads-oauth#' \
    configs/config.oauth.demo.yaml > "$OAUTH_CFG"

log "启动 mock OAuth provider (:9101)"
python3 "$ROOT/test/regression/mock_oauth.py" &
PIDS="$PIDS $!"

log "启动主服务 (:9000)"
"$BIN" -config "$MAIN_CFG" > "$WORK/main.log" 2>&1 &
PIDS="$PIDS $!"

log "启动 OAuth 演示实例 (:9002)"
"$BIN" -config "$OAUTH_CFG" > "$WORK/oauth.log" 2>&1 &
PIDS="$PIDS $!"

log "等待服务就绪"
wait_http() {
  local url="$1" name="$2" i
  for i in $(seq 1 40); do
    if curl -sf -o /dev/null "$url"; then log "$name 就绪"; return 0; fi
    sleep 0.5
  done
  log "$name 启动超时，日志：" && tail -20 "${3:-/dev/null}" || true
  return 1
}
wait_http http://127.0.0.1:9101/oidc/userinfo "mock provider"
wait_http http://127.0.0.1:9000/healthz "主服务" "$WORK/main.log"
wait_http http://127.0.0.1:9002/api/v1/oauth/providers "OAuth 实例" "$WORK/oauth.log"

BASE_MAIN=http://127.0.0.1:9000
BASE_OAUTH=http://127.0.0.1:9002
FAILED=0
run() {
  local name="$1" base="$2" script="$3"
  log "===== $name ====="
  if TENON_BASE="$base" TENON_NEWS_DB="$WORK/main.db" python3 "$ROOT/test/regression/$script"; then
    log "$name 通过"
  else
    log "$name 失败"
    FAILED=$((FAILED + 1))
  fi
}

run "会员体系"       "$BASE_MAIN/api/v1"  verify_membership.py
run "API 密钥"       "$BASE_MAIN"  verify_apikey.py
run "资讯聚合"       "$BASE_MAIN"  verify_news.py
run "来源分类白名单" "$BASE_MAIN"  verify_news_taxonomy.py
run "运维（备份/迁移/文档）" "$BASE_MAIN" verify_ops.py
run "配置化 OAuth"   "$BASE_OAUTH" verify_oauth.py

if [ "$FAILED" -gt 0 ]; then
  log "有 $FAILED 套回归失败"
  exit 1
fi
log "全部回归脚本通过"
