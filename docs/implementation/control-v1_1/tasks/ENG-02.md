# ENG-02 · 授权合同版本化与服务端写权限

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-01。

## 输入与实际代码入口
- `db/schema.sql`
- `db/company_scope.go`
- `db/task_scope.go`
- `db/tasks.go`
- `server/server.go`
- `intercept/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- db/task_control_contracts.go（建议新增）
- 人工合同批准/撤销服务与鉴权测试
- task scope 仅覆盖统计的兼容说明

## 实现步骤
1. 扩展嵌入式幂等 schema；不另建迁移框架。合同主键为 task_id+version，批准正文不可变；撤销和 active 指针独立审计。
2. 批准和撤销必须来自认证的授权管理入口；不得暴露成模型工具。
3. 合同包含方法/工具白名单、目标、身份、预算、数据范围、期限与冻结验收版本；合同缺失或过期默认拒绝。
4. 所有批准/撤销与任务状态变化锁相同 tasks 行并递增 control_epoch；队列和许可绑定版本。
5. 历史 AddAutoScope/AddAgentScope 只能改变覆盖统计，不能修改合同；存量运行任务部署前排空或显式隔离。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlContractVersionImmutable` | `db` | 批准正文修改失败；新版本只能追加。 |
| `TestControlCoverageDoesNotAuthorize` | `db` | 新增覆盖资产后授权结果不变。 |
| `TestControlAgentCannotApproveContract` | `server` | 模型/普通 Worker 身份不能批准、撤销或扩权。 |
| `TestControlRevocationEpoch` | `db` | 撤销产生新 epoch，旧许可失效。 |

本票用例清单：`acceptance/ENG-02.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
保留新合同历史；关闭新任务入口或进入只读，而非恢复缺省放行。

## 完成证据与交接
填写 `reports/ENG-02/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
