# ENG-14 · 单一结案事务与诚实部分结果

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-02, ENG-04, ENG-08, ENG-10。

## 输入与实际代码入口
- `db/task_completion.go`
- `db/tasks.go`
- `harness/completion.go`
- `agent/completion_guard.go`
- `server/engine.go`
- `report/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- TryCloseTask 与 CompleteTaskGuarded 共同内部实现
- run/result/reason/cleanup 正交字段
- 旧 tasks.status 映射与无模型报告

## 实现步骤
1. 保留唯一终态写入入口；在同一 task 行锁下先关新派发，再检查运行中动作、相关证据/验收版本与清理。
2. direct 完成指定任务不补全局覆盖；goal 报告有效回答或未知；coverage 对冻结覆盖矩阵统计。
3. 预算到限/授权撤回允许 partial/inconclusive/aborted，不要求所有未知问题清零。
4. 旧 status：complete→done，time budget→timeout，其余 partial/aborted→failed 作为 legacy 概括；新 API 必须展示 outcome/reason，不能让 UI 将 failed 误报系统故障。
5. cleanup unconfirmed 可持久 closed+人工移交，但不能显示全部清理完成；宿主从数据生成报告，LLM 不可用也能收尾。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlClosePartialOnBudget` | `db` | 未决问题存在且预算耗尽仍可正确结案。 |
| `TestControlDirectNoAutoExpansion` | `db` | 直投任务不生成额外覆盖任务。 |
| `TestControlLateCompletionCannotOverwrite` | `db` | 迟到 met 不能覆盖 timeout/revoked 终态。 |
| `TestControlCloseRechecksEvidence` | `db` | 验收前证据失效，complete 被拒绝。 |
| `TestControlReportWithoutLLM` | `server` | 无模型也可输出结果、未知项和清理状态。 |

本票用例清单：`acceptance/ENG-14.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
保留新旧字段同事务维护；不可把 partial 映射成完整成功以适配旧界面。

## 完成证据与交接
填写 `reports/ENG-14/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
