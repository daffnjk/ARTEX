# ARTEX research branch runbook / rollback / handoff（FINAL 汇编）

状态：ASSEMBLED（随里程碑推进滚动更新；本文件是 FINAL 验收项之一）

## 1. 环境与前置

- 分支 `research/autopilot-v2`（基点 main = cc8acc9d）；绝不写 main、不合并、不部署。
- 本地 PostgreSQL（17.11）+ 隔离研究库 `artex_research_test*`（可用 artex_research_test_suffix
  开平行库）；操作库 `artex` 只读 `llm_profiles`。
- Go ≥1.23（go.mod 钉 1.26.3；本机 1.27.0 可用）。
- 模型预算：仅 GLM-5.3-Flash profile；一切研究调用必须经 metered 包装（硬顶）；
  key 运行时读取，绝不打印/落盘。

## 2. 权威验证命令

```bash
# 离线全量（无 PG）
go build ./... && go vet ./... && go test -race ./harness ./cmd/harness-eval ./agent

# 研究矩阵（真实 PG；脚本内置 DSN 门卫，拒绝非本地/非 artex_research_test*）
ARTREX_TEST_DSN='postgres://'$USER'@localhost:5432/artex_research_test' \
  scripts/test-research-pg.sh
# 当前基线：34 db cases PASS（含 archive 回归，skip=fail）

# 计量研究运行器（预算硬顶，见 task-specs/M4.2.md 预注册）
go build -o /tmp/research-baseline ./cmd/research-baseline
go build -o /tmp/research-reviewer ./cmd/research-reviewer
go build -o /tmp/research-ablation  ./cmd/research-ablation
```

## 3. 研究构件地图（是什么/在哪/怎么复现）

| 构件 | 位置 | 复现入口 |
|------|------|----------|
| 验收器内核（绑定/失效码） | harness/ | go test ./harness |
| 计划持久化 CAS（M1b.1） | db/task_plans.go, agent/plantodo.go | TestTaskPlanPG* |
| 图原子写/对账（M1b.2） | db/exploration_atomic.go, graph_integrity.go | TestGraphOps/Integrity |
| 守门完成（M1b.3） | db/task_completion.go, server/engine.go | TestCompleteTaskGuarded |
| 不可变观测/验收（M2.1–2.2） | db/harness_store.go, harness_acceptance.go | TestHarness* |
| 巡检查口（M2.3） | server/research_inspection.go | inspection assembly in matrix |
| 场景基线（M0.1–0.2） | harness 16 场景 + cmd/research-baseline | evidence/M0-*.json |
| 假设/预注册实验（M3.1） | db/hypotheses.go, agent/hypothesis_tools.go | TestHypothesisLifecycle |
| 条件失效+唤醒（M3.2） | db/invalidation*.go, notification.go, server | TestConditionInvalidationLoop |
| 可解释选择（M3.3） | db/experiment_selection.go | TestExperimentSelection |
| 干净上下文审查（M3.4） | db/review_packet.go, cmd/research-reviewer | TestReviewPacket + live evidence |
| 提案挖掘（M4.1） | db/proposal_mining.go | TestMineChangeProposals |
| 固定预算消融（M4.2） | cmd/research-ablation, agent SetCognition | evidence/M4-2-*.json |
| 证据链闭合（批审 P1） | db/hypotheses.go RecordExperimentOutcome | TestSupportedHypothesisInherits* |
| 派发边界硬拒绝（批审 P2） | db/experiment_selection.go denyConflict, db/hypotheses.go DispatchExperiment | TestDispatchExperimentEnforces* |
| 级联删除处置认知史（批审 P2） | db/tasks.go dropCognitionHistory | TestDeleteTaskCascadeHandlesCognitionHistory |

## 4. 回滚

- 代码：常规 git revert（分支线性历史；无 force-push；无部署面）。
- 研究库：`artex_research_test` 为一次性库——drop & recreate 即回滚
  （schema 由 db.Open 幂等应用）。操作库 `artex` 从未被本分支写入。
- 追加式表（hypotheses/experiments/harness_observations 等）有触发器防改写；
  一次性维护需 `SET session_replication_role = replica`（先例：M2.1 去重、M4.2 清理），
  且仅限研究库。
- 模型预算：无"回滚"，只有熔断——metered 硬顶触顶即停（预注册在 task-specs/）。

## 5. 交接（下一会话从这里继续）

- 状态：docs/autopilot/STATE.json（single source of truth）；证据在 evidence/；
  决策与教训在 DECISIONS.md；本文件为运行手册。
- 待办池：BACKLOG.json（DRAFT 项含 FINAL 收尾清单）。
- 已知未做（诚实边界）：多轮 worker 闭环消融（见 M4.2 预注册"已知边界"）；
  M3.2–M4.1 独立审查记录见 evidence/；5 个 server 测试为既有环境失败（与本分支无关，
  git stash 对照验证过）。
