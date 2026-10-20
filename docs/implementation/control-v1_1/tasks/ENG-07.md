# ENG-07 · 贯穿真实 Worker 和所有任务执行入口

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-05, ENG-06。

## 输入与实际代码入口
- `agent/worker.go`
- `server/engine.go`
- `server/assembly.go`
- `guard/guard.go`
- `agent/augment.go`
- `enrich/`
- `mcphttp/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 新模式实际装配图
- 受限工具目录与能力拒绝
- 首条真实 Worker→本地 adapter 集成测试

## 实现步骤
1. runIntent 与 Worker tool call 将 TaskContext/Execution/Attempt/Grant 贯通，不能仅在提示词加入许可文字。
2. 宿主运行态记录 task/exploration/parent job，禁止通过缺失 task_id 调用共享 unrestricted 工具逃离预算。
3. 自主模式中只装配已审适配器；Bash、浏览器、远程 MCP、自定义命令、平台后台补全若未适配，明确禁用而非绕过；人工直投/retest/enrich 同规。
4. 通过脚本化模型驱动真实 Planner/Worker 控制流，工具实际请求本地 fixture；这是集成测试，不宣称是真实 LLM 行为实验。
5. API/后台入口的拒绝也留审计；新模式启用前完成入口清单签核。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlRealWorkerUsesGrant` | `server` | 实际 Worker 接口调用经授予和 adapter，local fixture 收到一个许可动作。 |
| `TestControlUnapprovedToolUnavailable` | `server` | 自主模式无法调用未验收 Shell/browser/MCP。 |
| `TestControlBackgroundPathsEnforced` | `server` | retest/enrich/人工直投外部请求不能走未受控路径。 |
| `TestControlMissingTaskContextDenied` | `server` | 丢失任务上下文的外部动作被拒绝。 |

本票用例清单：`acceptance/ENG-07.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
停止新模式派发并保留运行记录；不切回旧无合同 Worker 继续同一任务。

## 完成证据与交接
填写 `reports/ENG-07/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
