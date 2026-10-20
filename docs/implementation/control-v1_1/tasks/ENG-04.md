# ENG-04 · 任务级预算预留、结算与模型调用归因

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-01, ENG-02。

## 输入与实际代码入口
- `db/llm_usage.go`
- `db/tool_usage.go`
- `llmrec/llmrec.go`
- `server/task_llm.go`
- `server/tool_usage.go`
- `agent/runinfo.go`
- `db/schema.sql`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 资源账户与预留账本
- Provider 调用边界预算包装
- 模型失败、重试和收尾计费策略

## 实现步骤
1. 原有 llm_usage 是事后计量且允许 best-effort 失败；保留它，但新增硬准入账本，不把事后写日志作为准入。
2. 资源账户用整数单位并带任务+维度唯一约束；预留/结算幂等键由宿主生成。
3. 每次实际 Provider 尝试（含 failover、摘要、审查）先预留；任务绑定角色遗漏时自主模式拒绝请求。
4. 精确可计数资源用上限控制；token 用可验证输入上界、输出上限及保守预留；未知 usage 保留预留不返还。无可靠上界的账单维度明确标 estimated，不宣称精确硬预算。
5. 为收尾和对账保留独立不可被 Planner 花费额度；用 fake clock 测试持久 deadline，进程重启不刷新额度。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlBudgetConcurrentReserve` | `db` | 并发预留总量不超过可用预算，失败无部分扣款。 |
| `TestControlBudgetSettlementIdempotent` | `db` | 重复结算不双扣、不双退款。 |
| `TestControlLLMRetryBudgeted` | `server` | 重试/切换/摘要每次调用都归因计量。 |
| `TestControlUnknownUsageNotRefunded` | `db` | 未知调用成本保守挂账，对账前不返还。 |
| `TestControlCloseReserveProtected` | `server` | 探索耗尽仍可无 LLM 生成结果并执行本地清理。 |

本票用例清单：`acceptance/ENG-04.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
保留账本；计费估算变更需版本，停用新派发而非关预算控制。

## 完成证据与交接
填写 `reports/ENG-04/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
