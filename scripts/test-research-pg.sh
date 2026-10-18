#!/bin/sh
# M1b.1 专用隔离 PostgreSQL 测试 runner（research）。
#
# 用法: ARTEX_TEST_PG_DSN='postgres://user@localhost:5432/artex_research_test' \
#       sh scripts/test-research-pg.sh
#
# 硬性要求（拒绝而非跳过）：
#   1. DSN 必须显式提供；
#   2. 主机必须是 localhost/127.0.0.1/::1（本地隔离库，绝不连远程/生产）；
#   3. 库名必须以 artex_research_test 开头（专用可重置测试库）。
# 满足后运行 task_plans 的完整 CAS/恢复/删除/归档矩阵（该套件本身也有同款守卫），
# 并以同一 DSN 激活 db 包既有的归档/任务 PG 回归。绝不使用生产 DSN。
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DSN=${ARTEX_TEST_PG_DSN:-}

fail() { echo "test-research-pg.sh: $*" >&2; exit 2; }

[ -n "$DSN" ] || fail "ARTEX_TEST_PG_DSN is required (e.g. postgres://user@localhost:5432/artex_research_test)"

# 纯 shell 解析（BSD/GNU sed 行为差异不可依赖）：先剥查询串再取最后一段路径。
nopq=${DSN%%\?*}
db=${nopq##*/}
hostpart=${nopq#*://}
hostpart=${hostpart%%/*}
hostpart=${hostpart##*@}
host=${hostpart%%:*}
host=${host#\[}
host=${host%\]}

case "$host" in
  ""|localhost|127.0.0.1|::1) ;;
  *) fail "refusing non-local host '$host' (isolated local test DB only)" ;;
esac
case "$db" in
  artex_research_test*) ;;
  *) fail "refusing database '$db' (must be named artex_research_test*)" ;;
esac

echo "== research PG matrix =="
echo "dsn target: host=${host:-local-socket} db=$db"

# 专用非跳过套件 + 既有 PG 回归（testDSN 读 ARTEX_PG_DSN）。
export ARTEX_PG_DSN="$DSN"
cd "$root"
go test -race -count=1 -v -run 'TestTaskPlanPG' ./db
go test -count=1 -run 'TestTaskArchiveDatabaseRoundTrip' ./db
echo "PASS: research PG matrix on $db"
