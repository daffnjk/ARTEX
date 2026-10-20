# ENG-10 · 将评估、信念和血缘更新收敛到一个事务

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-09。

## 输入与实际代码入口
- `db/hypotheses.go`
- `db/harness_store.go`
- `db/harness_acceptance.go`
- `agent/hypothesis_tools.go`
- `harness/evaluation.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- CommitValidatedOutcome（建议新增）
- 事务内 evaluation+claim+dependency+event 写入
- 旧字符串入口只供 legacy fixture 且不能影响新任务

## 实现步骤
1. 版本化验证器先检查前提与原文；收集 validated evaluation，再在事务内重查上下文/证据仍有效。
2. 在同一事务记录正式 evaluation、结论与证据依赖、反证/失效及 outbox；不得调用另起事务的公共方法假装原子。
3. 优先将 StoreHarnessObservation/RecordEvaluation 的必要 SQL 抽为内部 tx helper，不破坏现有 append-only 与 request_id 去重。
4. 除非方法声明且验证互斥，支持一个假设不自动反驳其余；invalid/unknown premise 只得 inconclusive。
5. 旧 RecordExperimentOutcome 保留研究兼容但明确版本隔离；新任务不能再从 observed 字符串直接更新正式 supported。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlOutcomeEvidenceRequired` | `db` | 缺失/非法证据时所有正式状态保持不变。 |
| `TestControlOutcomeAtomicRollback` | `db` | 任一依赖或事件写失败，评估与假设更新同时回滚。 |
| `TestControlNonexclusiveHypotheses` | `db` | 一条支持不使非互斥替代解释自动 refuted。 |
| `TestControlExpiredIdentityInconclusive` | `db` | 失效身份的拒绝响应不能得有效阴性。 |
| `TestControlInvalidateEvidencePropagates` | `db` | 证据撤回使派生结论失效并唤醒计划。 |

本票用例清单：`acceptance/ENG-10.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
新模式继续要求证据前置；旧研究模式保留独立标识，不作为生产回退。

## 完成证据与交接
填写 `reports/ENG-10/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
