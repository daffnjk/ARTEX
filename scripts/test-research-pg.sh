#!/bin/sh
# M1b.1 专用隔离 PostgreSQL 测试 runner（research）。
#
# 用法: ARTEX_TEST_PG_DSN='postgres://user@localhost:5432/artex_research_test' \
#       sh scripts/test-research-pg.sh
#
# 硬性要求（拒绝而非跳过）：
#   1. DSN 必须显式提供，且不得携带查询参数（pgx 会应用 ?host=/?database=
#      覆盖项，只看 authority 的守卫可被绕过）；
#   2. 主机必须是 localhost/127.0.0.1/::1（本地隔离库，绝不连远程/生产）；
#   3. 库名必须以 artex_research_test 开头（专用可重置测试库）。
# 满足后运行 task_plans 的完整 CAS/恢复/删除/归档矩阵（该套件本身也有同款守卫），
# 并以同一 DSN 激活 db 包既有的归档 PG 回归——回归若因库不可达而 SKIP 同样判失败
#（专用 runner 里不允许静默跳过）。绝不使用生产 DSN。
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DSN=${ARTEX_TEST_PG_DSN:-}

fail() { echo "test-research-pg.sh: $*" >&2; exit 2; }

[ -n "$DSN" ] || fail "ARTEX_TEST_PG_DSN is required (e.g. postgres://user@localhost:5432/artex_research_test)"
case "$DSN" in
  *\?*) fail "DSN with query parameters is not accepted (pgx ?host=/?database= overrides bypass the guard); pass a plain URL" ;;
esac

# 纯 shell 解析（BSD/GNU sed 行为差异不可依赖）：查询串已被拒绝，直接取路径段。
db=${DSN##*/}
hostpart=${DSN#*://}
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

cd "$root"

# 专用非跳过套件（-race）。
ARTEX_TEST_PG_DSN="$DSN" go test -race -count=1 -v -run 'TestTaskPlanPG|TestGraphOps|TestGraphIntegrity|TestSoftDeleteRepeat|TestHarnessObservation|TestHarnessResolver|TestHarnessAcceptance|TestCompleteTaskGuarded' ./db

# 既有归档回归（testDSN 读 ARTEX_PG_DSN）：专用 runner 里 SKIP = 失败。
ARTEX_PG_DSN="$DSN" go test -count=1 -v -run 'TestTaskArchiveDatabaseRoundTrip' ./db > /tmp/artex-research-pg-archive.log 2>&1 || {
  cat /tmp/artex-research-pg-archive.log
  fail "archive regression FAILED"
}
cat /tmp/artex-research-pg-archive.log
if grep -Eq "SKIP|no database config|postgres unavailable" /tmp/artex-research-pg-archive.log; then
  fail "archive regression SKIPPED (database unreachable) — not acceptable in the dedicated runner"
fi
echo "PASS: research PG matrix on $db"
