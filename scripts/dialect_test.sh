#!/usr/bin/env bash
# 跨方言契约测试：起临时 MySQL / PostgreSQL 容器，注入 DSN 跑 test/dialect_test.go，结束后清理。
# 用法：bash scripts/dialect_test.sh [mysql|postgres|all]（默认 all）
set -euo pipefail

cd "$(dirname "$0")/.."
TARGET="${1:-all}"

MYSQL_CONTAINER="tenon-dialect-mysql"
PG_CONTAINER="tenon-dialect-pg"
MYSQL_PORT="${TENON_TEST_MYSQL_PORT:-13306}"
PG_PORT="${TENON_TEST_PG_PORT:-15432}"
MYSQL_PASS="tenontest"
PG_PASS="tenontest"

cleanup() {
  echo "==> 清理测试容器"
  docker rm -f "$MYSQL_CONTAINER" "$PG_CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

wait_mysql() {
  for i in $(seq 1 90); do
    if docker exec "$MYSQL_CONTAINER" mysqladmin ping -h 127.0.0.1 -uroot -p"$MYSQL_PASS" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "MySQL 启动超时" >&2
  return 1
}

wait_pg() {
  for i in $(seq 1 60); do
    if docker exec "$PG_CONTAINER" pg_isready -U postgres -d tenon >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "PostgreSQL 启动超时" >&2
  return 1
}

ENVS=()

if [[ "$TARGET" == "mysql" || "$TARGET" == "all" ]]; then
  echo "==> 启动 MySQL 8.0 (host $MYSQL_PORT)"
  docker rm -f "$MYSQL_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$MYSQL_CONTAINER" \
    -e MYSQL_ROOT_PASSWORD="$MYSQL_PASS" -e MYSQL_DATABASE=tenon \
    -p "$MYSQL_PORT:3306" mysql:8.0 >/dev/null
  wait_mysql
  ENVS+=("TENON_TEST_MYSQL_DSN=root:${MYSQL_PASS}@tcp(127.0.0.1:${MYSQL_PORT})/tenon")
fi

if [[ "$TARGET" == "postgres" || "$TARGET" == "all" ]]; then
  echo "==> 启动 PostgreSQL 16 (host $PG_PORT)"
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$PG_CONTAINER" \
    -e POSTGRES_PASSWORD="$PG_PASS" -e POSTGRES_DB=tenon \
    -p "$PG_PORT:5432" postgres:16-alpine >/dev/null
  wait_pg
  ENVS+=("TENON_TEST_PG_DSN=postgres://postgres:${PG_PASS}@127.0.0.1:${PG_PORT}/tenon?sslmode=disable")
fi

echo "==> 运行跨方言契约测试: ${ENVS[*]}"
env "${ENVS[@]}" GOTOOLCHAIN=local go test ./test/ -run TestDialect -v -count=1
echo "==> 跨方言契约测试完成"
