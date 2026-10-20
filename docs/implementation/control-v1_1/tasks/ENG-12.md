# ENG-12 · 语义重复控制与合理复测例外

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-05, ENG-11。

## 输入与实际代码入口
- `db/intent_admission.go`
- `db/experiment_selection.go`
- `agent/tools.go`
- `control/（新增）`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 语义动作键与幂等键分离
- 结构化复测原因
- 返回已有结果与在途执行引用

## 实现步骤
1. 签名包含规范化资产/身份/方法及版本/关键参数/相关前提版本/测试目的；不包括随机 nonce 或全部全局状态版本。
2. 唯一在途执行约束放数据库，避免两个 Planner 检查后同时插入。
3. 既有有效结果可直接引用；进行中返回原 execution；旧证据失效/部署变化/独立确认可申请复测。
4. 复测理由由宿主验证，不以模型自由文案允许绕过去重；次数仍扣预算。
5. 区分 valid negative 与无效实验；不建立跨任务五次阴性跳过漏洞类规则。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlSemanticDedupConcurrent` | `db` | 不同文案描述同一动作，并发只能授予一次。 |
| `TestControlRunningActionReused` | `db` | 重复提议返回已有 execution_id。 |
| `TestControlRetestRequiresReason` | `db` | 无有效版本变化/批准复测理由不放行。 |
| `TestControlContextChangeAllowsBoundedRetest` | `db` | 合法条件变化可重验，预算照扣。 |

本票用例清单：`acceptance/ENG-12.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
允许回退更保守优先级，但保留在途唯一约束；不能删除执行历史。

## 完成证据与交接
填写 `reports/ENG-12/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
