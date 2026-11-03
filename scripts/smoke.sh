#!/usr/bin/env bash
# 冒烟测试：登录后依次验证关键接口，任一失败即退出非 0。
# 用法：BASE=http://127.0.0.1:9000 USER=admin PASS='Admin@123456' ./scripts/smoke.sh
set -euo pipefail

BASE="${BASE:-http://127.0.0.1:9000}"
USER_NAME="${USER:-admin}"
PASS="${PASS:-Admin@123456}"

pass=0
fail=0

say() { printf '%-46s %s\n' "$1" "$2"; }

check() { # check <描述> <期望状态码> <方法> <路径> [body]
  local desc="$1" want="$2" method="$3" path="$4" body="${5:-}"
  local args=(-s -o /tmp/.smoke.out -w '%{http_code}' -X "$method" "$BASE$path")
  [[ -n "${TOKEN:-}" ]] && args+=(-H "Authorization: Bearer $TOKEN")
  if [[ -n "$body" ]]; then
    args+=(-H 'Content-Type: application/json' -d "$body")
  fi
  local code
  code=$(curl "${args[@]}" || echo 000)
  if [[ "$code" == "$want" ]]; then
    say "✓ $desc" "[$code]"
    pass=$((pass + 1))
  else
    say "✗ $desc" "[$code 期望 $want] $(head -c 160 /tmp/.smoke.out)"
    fail=$((fail + 1))
  fi
}

echo "== 健康检查 =="
check "GET /healthz" 200 GET /healthz
check "GET /readyz" 200 GET /readyz

echo "== 认证 =="
check "错误密码返回 401" 401 POST /api/v1/auth/login '{"username":"admin","password":"bad-password"}'
check "未带令牌访问受保护接口" 401 GET /api/v1/users

TOKEN=$(curl -s -X POST "$BASE/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"$USER_NAME\",\"password\":\"$PASS\"}" \
  | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')
if [[ -z "$TOKEN" ]]; then
  echo "✗ 登录失败，无法继续" >&2
  exit 1
fi
say "✓ 登录成功" "[token ${#TOKEN} 字符]"
pass=$((pass + 1))

check "GET /auth/profile" 200 GET /api/v1/auth/profile

echo "== 业务接口 =="
check "GET /users" 200 GET "/api/v1/users?page=1&page_size=5"
check "GET /roles" 200 GET "/api/v1/roles?page=1&page_size=5"
check "GET /menus/tree" 200 GET /api/v1/menus/tree
check "GET /depts/tree" 200 GET /api/v1/depts/tree
check "GET /dicts/types" 200 GET "/api/v1/dicts/types?page=1&page_size=5"
check "GET /configs" 200 GET "/api/v1/configs?page=1&page_size=5"
check "GET /notices" 200 GET "/api/v1/notices?page=1&page_size=5"
check "GET /messages" 200 GET "/api/v1/messages?page=1&page_size=5"
check "GET /messages/unread-count" 200 GET /api/v1/messages/unread-count
check "GET /messages/recent" 200 GET "/api/v1/messages/recent?limit=5"
check "GET /messages/sent" 200 GET "/api/v1/messages/sent?page=1&page_size=5"
check "GET /jobs" 200 GET "/api/v1/jobs?page=1&page_size=5"
check "GET /files" 200 GET "/api/v1/files?page=1&page_size=5"
check "GET /logs/login" 200 GET "/api/v1/logs/login?page=1&page_size=5"
check "GET /logs/operation" 200 GET "/api/v1/logs/operation?page=1&page_size=5"
check "GET /online/sessions" 200 GET "/api/v1/online/sessions?page=1&page_size=5"
check "GET /dashboard/overview" 200 GET /api/v1/dashboard/overview
check "GET /monitor/system" 200 GET /api/v1/monitor/system

echo "== 参数校验 =="
check "缺少必填字段返回 400" 400 POST /api/v1/users '{"username":"ab"}'

echo "== 静态资源 =="
check "GET /" 200 GET /
# 从首页 HTML 里取出实际产物名（Vite 构建带内容 hash）
ASSET_JS=$(curl -s "$BASE/" | grep -o '/assets/index-[A-Za-z0-9_-]*\.js' | head -1 || true)
if [[ -n "$ASSET_JS" ]]; then
  check "GET $ASSET_JS" 200 GET "$ASSET_JS"
else
  say "✗ 首页未引用构建产物" "[缺少 /assets/index-*.js]"
  fail=$((fail + 1))
fi

echo
printf '通过 %d 项，失败 %d 项\n' "$pass" "$fail"
[[ "$fail" -eq 0 ]]
