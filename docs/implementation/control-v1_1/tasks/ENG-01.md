# ENG-01 · 冻结领域契约、错误码和单体依赖方向

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-00。

## 输入与实际代码入口
- `agent/runinfo.go`
- `db/tasks.go`
- `db/task_plans.go`
- `harness/evaluation.go`
- `db/schema.sql`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- contracts/ 的实现对照 ADR
- control/ 中的无数据库领域类型与纯校验（建议新增）
- 任务上下文和版本字段

## 实现步骤
1. 统一 TaskID、ExplorationID、ExecutionID、AttemptID 的边界；task→exploration 由宿主查询，不接收模型自报归属。
2. 固定 direct/goal/coverage、三维执行结果、评估有效性、结案口径和错误码。
3. 定义 ActionProposal 不包含可自填的批准者、可信来源或许可字段；actor 来源于认证上下文。
4. 冻结导入方向：control 仅标准库/纯类型；execution 依赖 control 与接口；db 实现端口；server 装配；禁止 db↔execution 环。
5. 确定相关上下文版本，不用全局每次写图版本使所有缓存失效；定义 JSON 大小、步数和字段白名单。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlProposalRejectsAuthorityFields` | `control` | 模型输入带 approved_by/grant/provenance 等权威字段时拒绝。 |
| `TestControlIDNamespaces` | `control` | 错误 ID 域不可被静默接受。 |
| `TestControlOutcomeDimensions` | `control` | 运行结果、外部效果、清理状态可独立表达。 |

本票用例清单：`acceptance/ENG-01.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
仅新增契约和版本字段，不自动转换历史状态；变更契约需新 ADR 与相关测试。

## 完成证据与交接
填写 `reports/ENG-01/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
