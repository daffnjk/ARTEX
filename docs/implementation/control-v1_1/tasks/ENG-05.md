# ENG-05 · 原子准入、许可与持久出站队列

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-02, ENG-03, ENG-04。

## 输入与实际代码入口
- `db/schema.sql`
- `db/intent_admission.go`
- `server/engine.go`
- `control/（新增）`
- `execution/（新增）`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- AdmitAndReserve 事务端口
- executions/attempts/outbox 数据结构
- 重复提交和动作摘要契约

## 实现步骤
1. 事务统一锁序：tasks → 活跃合同 → 资源账户按维度排序 → execution → 相关实验/节点；所有状态写入口遵守同一顺序。
2. 锁内检查任务仍接受新工作、合同 epoch/版本、审批、方法能力、依赖版本与预算；冻结规范化动作摘要。
3. 同事务预留资源、写 execution/grant/outbox；提交前禁止网络调用，事务失败不得留下可执行事件。
4. 重复幂等键+相同摘要返回原 ID；同键不同摘要冲突。过期许可只返回当前不可执行状态，不变相重新批准。
5. dispatcher 仅领取已提交 outbox；重复通知允许，逻辑动作幂等消费；执行前再次检查 epoch。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlAdmissionRollback` | `db` | 在预留/执行/出站写入处故障注入，结果全回滚。 |
| `TestControlAdmissionReplay` | `db` | 相同请求仅一条 execution 和预算预留。 |
| `TestControlIdempotencyConflict` | `db` | 同键异参数明确拒绝。 |
| `TestControlCloseAdmissionRace` | `db` | 结案与准入竞争时不可在关闭后授予新动作。 |
| `TestControlRevokeAdmissionRace` | `db` | 撤销先提交则无法授予旧版本动作。 |

本票用例清单：`acceptance/ENG-05.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
暂停 dispatcher 并对账已授予动作；不删除在途记录，不自动重放。

## 完成证据与交接
填写 `reports/ENG-05/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
