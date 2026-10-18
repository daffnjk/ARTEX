# Task plan persistence (M1b.1) — runbook

研究分支 `research/autopilot-v2` 引入的 planner 计划持久化。本手册覆盖：变更面、
迁移、验证命令、回滚、边界与已知限制。

## 变更面

- `db/schema.sql`：新表 `task_plans`（每探索一行，`revision` CAS 计数器，
  `steps` JSONB 携带宿主分配的稳定步骤 ID + 可选 `basis_node_ids`/`expected_output`，
  `model_notes` 单独存放）。幂等 `CREATE TABLE IF NOT EXISTS`，随项目既有迁移
  路径（`db.Open` 在 advisory lock 下应用 schema）自动落地。
- `db/task_plans.go`：`LoadTaskPlan`（无记录 = 显式空计划）、`SaveTaskPlan`
  （CAS：期望 revision 不匹配 ⇒ `*PlanConflictError`，绝不 last-write-wins；
  先持久化后返回）、`MergePlanSteps`（内容精确匹配保 ID，顺序无关；新内容新
  ID；改写文本 = 新步骤——SDK 整表替换接口的已知限制）、`ValidatePlanSteps/
  Notes`（步骤数/内容/便签/basis 数量边界）、`PlanBasisNodesOwned`（basis
  归属校验，拒绝跨任务引用）。
- `agent/plantodo.go` + `agent/planner.go`：持久化模式 = `Options.Todos` 置 nil
  （SDK 工具与 reminder 让位，SDK 有 nil 保护），注册自研 `TodoWrite`
  （真实 CoreTool 路径，经 AugmentTools/ToolResolve，tools 表无该行故原样放
  行）。`Plan()` 开头 hydrate 权威计划——失败即停止本轮（fail-closed，绝不回
  退空内存）；每次 TodoWrite 先 CAS 落盘、后确认（ack 带落盘 revision）；冲突
  与存储错误均显式报给模型。内存模式（`SetPlanStore` 未设）保持原行为，供
  离线/测试。
- `server/task_llm.go`：任务 bundle 的 planner 总是 `SetPlanStore(s.m.pg)`。
- `db/task_archives_restore.go`：归档清除枚举加入 `task_plans`（计划是可再生
  草稿，不进归档包；恢复后 hydrate 出显式空计划，格式版本不变）。
- `.github/workflows/research-harness.yml`：新增 `research-postgres` job
  （postgres:16-alpine 服务容器 + 专用 runner）。

## 迁移

- 应用现有版本启动即自动迁移（`db.Open` 幂等应用 schema.sql）；无独立迁移
  脚本、无数据搬运（新表，旧行为零）。
- 迁移可重复执行：`Open` 两次连跑已由 `TestTaskPlanPGMigrationIdempotent`
  在真实 PG 上验证（含两次迁移之间写入的数据不被损伤）。

## 验证命令

```sh
# 专用隔离 PG 矩阵（runner 拒绝缺失 DSN 与任何非本机/非 artex_research_test* 目标）
ARTEX_TEST_PG_DSN='postgres://<user>@localhost:5432/artex_research_test' \
  sh scripts/test-research-pg.sh

# 离线单元（合并/校验/工具语义，无 PG）
go test -race -count=1 -run 'TestMergePlanSteps|TestValidatePlanSteps' ./db
go test -race -count=1 -run 'TestPlannerTodo|TestLoadPlannerPlanState|TestPlannerTodosProjection' ./agent
```

本地已验证环境：PostgreSQL 17.11（Homebrew，artex_research_test 隔离库）、
Go 1.27.0（项目工具链 1.26.3 兼容编译）。CI（research-postgres job，PG16）在
分支推送后由 GitHub Actions 执行。

## 回滚

计划是可再生的模型草稿，回滚只影响这张表：

```sh
# 可选：保留现存计划（推荐先做）
pg_dump -h localhost -U <user> -d <db> -t task_plans > task_plans-backup.sql

# 显式回滚（有日志、有确认，绝不静默丢弃）
psql -h localhost -U <user> -d <db> -c 'DROP TABLE IF EXISTS task_plans;'
```

代码回滚 = revert 引入提交（`24a6337`、`d12f2ff`）；planner 回到内存模式
（`SetPlanStore` 随 revert 移除后自然回退；单独禁用持久化可在装配处不调用
`SetPlanStore`）。表与代码可独立回滚：表在而代码不读 = 无害死数据；代码在而
表缺 = hydrate 报错停轮（fail-closed，符合设计）。

## 边界与已知限制

- 步骤身份 = 内容精确匹配；模型改写步骤文本会产生新步骤 ID（SDK TodoWrite
  整表替换且无 ID 概念，无法表达"同一步骤改文案"）。
- 计划状态（completed 等）只是模型便签：任何验收/完成路径（prove_goal、
  completion guard）都不读 `task_plans`——按 M1a.1 契约，验收绑定可信冻结
  目标，与计划勾选无关。
- 计划修订历史（审计回放）未建表，推迟到 M3（信念修订需要时再加）。
- basis 的 `expected_output` 在 M1b.1 只记录不绑定；绑定到真实产出属 M2/M3。
- 完整 LLM 循环内的端到端 planner 恢复（真模型驱动 Plan() 后重启再 hydrate）
  未在本里程碑跑过——存储层与工具层的恢复语义已分别在真实 PG 与单元层验证；
  端到端留到 M2.3/M3 集成（如实记录，不冒充已验证）。
