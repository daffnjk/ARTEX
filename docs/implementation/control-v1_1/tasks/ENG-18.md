# ENG-18 · 完整真实 Worker 闭环与故障 CI 矩阵

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-13, ENG-15, ENG-16, ENG-17。

## 输入与实际代码入口
- `harness/scenarios/`
- `server/*_test.go`
- `scripts/test-research-pg.sh`
- `.github/workflows/research-harness.yml`
- `docs/autopilot/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- CI 必测清单与 JSON 判定（零缺席、零必测 skip）
- 本地正/负/无效/崩溃/撤销场景
- 独立整合审查产物

## 实现步骤
1. 使用脚本化 Provider+真实 Worker+本地 HTTP fixture+隔离 PG；明确与真实模型试验的区分。
2. 同场景覆盖有效成功、有效阴性、会话失效、限速、截断、重复通知、崩溃、撤销、预算和提示注入文本。
3. 目标侧命中计数与 DB 事件一起验收；fake adapter 单测不能代替网络集成。
4. 故障矩阵固定版本和随机种子；必测集合完整出现且均通过，错误退出或 skip 即 FAIL。
5. 在 CI 只授予测试实例最小权限；没有测试密钥/模型时真实模型项 NOT_RUN，不伪称完整智能行为验证。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlE2EValidAnswer` | `server` | 真实 Worker 获取证据、正式评估、调整计划并按契约关闭。 |
| `TestControlE2EExpiredSession` | `server` | 失效会话不形成阴性；修复前提后重验。 |
| `TestControlE2ERevocation` | `server` | 撤销后无新请求，清理情况真实。 |
| `TestControlE2EUnknownEffect` | `server` | 崩溃恢复不会重复外部计数效果。 |
| `TestControlE2EBudgetPartial` | `server` | 预算耗尽准确 partial，剩余问题与账本可检查。 |
| `TestControlE2EUntrustedContent` | `server` | 不可信响应中的越权指令不改变许可或调用目录。 |

本票用例清单：`acceptance/ENG-18.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
任何安全/证据/取消红线失败均冻结自主执行；不得降低用例或跳过测试解除失败。

## 完成证据与交接
填写 `reports/ENG-18/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
