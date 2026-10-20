# ENG-13 · 反馈驱动短窗口规划与评分影子模式

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-07, ENG-08, ENG-10, ENG-11, ENG-12。

## 输入与实际代码入口
- `agent/planner.go`
- `db/experiment_selection.go`
- `server/engine.go`
- `agent/hypothesis_tools.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 反馈类型→策略动作表
- 收结果队列与新实验队列分离
- 版本化评分记录与停滞计数

## 实现步骤
1. 先消费结果/撤权/失效，再建立快照和生成少量候选；不用固定 heartbeat 代替紧急处理。
2. 识别 VALID_NEGATIVE、PRECONDITION_INVALID、TOOL_UNAVAILABLE、WAIT_EXPIRED、POLICY_DENIED、RATE_LIMITED、EFFECT_UNKNOWN 等类别。
3. 先硬过滤再算代理分数；评分用可解释的区分能力/目标/覆盖/成本/风险/重复，不叫已校准 EIG。
4. 默认 shadow：记录建议、不偷偷改变派发；多假设但同预测不给高区分分。
5. 停滞按无新增有效证据与预算消耗识别；意外且有效结果不扣失败分；Planner 在受控窗口内重算。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlScoringRespectsHardFilter` | `db` | 高收益不能让被授权拒绝动作进入候选。 |
| `TestControlSamePredictionsNoDiscrimination` | `db` | 多个同预测假设无额外区分奖励。 |
| `TestControlUnexpectedEvidenceReplans` | `agent` | 有效意外结果触发更新而非盲目惩罚分支。 |
| `TestControlTypedFailureHandling` | `server` | 等待超时收原结果，策略拒绝不重试绕行，限速冷却。 |
| `TestControlPlannerStallBound` | `agent` | 重复空转有界，进入清晰暂停/收尾原因。 |

本票用例清单：`acceptance/ENG-13.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
仅评分/摘要/策略旗标可回滚为基线；授权、证据、去重在途约束不得关闭。

## 完成证据与交接
填写 `reports/ENG-13/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
