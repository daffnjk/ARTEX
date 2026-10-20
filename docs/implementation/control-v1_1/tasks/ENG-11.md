# ENG-11 · 计划稳定 ID 原位修改与依赖有效性门

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-01, ENG-10。

## 输入与实际代码入口
- `db/task_plans.go`
- `agent/plantodo.go`
- `agent/planner.go`
- `db/graph_integrity.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- PatchPlanStep 保留 RowID+Revision CAS
- requires_step/claim 与上下文版本
- 派发依赖检查与循环检测

## 实现步骤
1. 当前 PlanStep.ID 与 RowID/CAS 已有；新增按 ID patch，不能重写整库或仅根据文案生成 ID。
2. 兼容旧 TodoWrite 的整体更新；新接口提交 step_id 和 expected revision，旧接口不允许静默丢弃新依赖字段。
3. 依赖检查同任务/允许来源、证据有效性、身份/目标版本与所需前置结果，拒绝缺失/过期依赖。
4. DAG 校验拒绝自环、多步环和超过现有容量上限；运行中改变前提使步骤重新 blocked。
5. 计划标 completed 是便签而非执行成功/目标证明；验收不信任 Planner 单方步骤状态。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlPlanPatchKeepsID` | `db` | 改文字仍保持 ID；旧 revision 或删除重建 RowID 被拒绝。 |
| `TestControlPlanCycleRejected` | `db` | 自环与多步依赖环拒绝。 |
| `TestControlDependencyContextRequired` | `db` | 事实存在但身份/环境版本不符仍不可派发。 |
| `TestControlTodoLegacyPreservesDependencies` | `agent` | 旧整体更新不丢依赖或绕过 ID 语义。 |

本票用例清单：`acceptance/ENG-11.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
保留新增依赖，回滚 UI 不回滚执行门；不允许旧写入器破坏新版本计划。

## 完成证据与交接
填写 `reports/ENG-11/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
