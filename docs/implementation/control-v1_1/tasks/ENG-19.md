# ENG-19 · 受控评测、交付验收与有限开放

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-18。

## 输入与实际代码入口
- `docs/autopilot/`
- `server/`
- `report/`
- `harness/scenarios/`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- release-readiness.json 与已知限制
- 按工具/目标/模型版本的开放矩阵
- 全成本对照与现场运行手册

## 实现步骤
1. 工程门禁先通过，再用同一安全底座比较候选评分/Reviewer 的效益；不关闭安全约束做有外部效果的消融。
2. 真实模型试验仅在用户批准的模型端点、合成数据和隔离环境进行；按审批 budget 有界重复，记录全部成本。
3. 没有 ground truth 的真实目标不报告 recall；行为试验与确定性集成试验分开。
4. 上线批准只覆盖已验收 HTTP 适配器和方法；Shell/browser/MCP 扩展为新版本，不从一次通过外推。
5. 交付测试原始日志、代码树哈希、迁移/回滚/取消演练与未实现表；不自动 push、开外网扫描或生产部署。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlReleaseAllowlist` | `server` | 启用范围严格等于获批版本/方法/adapter，不隐式扩大。 |
| `TestControlFeatureRollbackKeepsSafety` | `server` | 评分或 Reviewer 策略回滚不能关闭授权、预算和证据门。 |

本票用例清单：`acceptance/ENG-19.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
真实模型/部署审批缺失时 release_status=READY_FOR_APPROVAL 或 BLOCKED，不自动签署上线。

## 回退
回到受控只读或停止新任务；运行中动作对账并保留审计。

## 完成证据与交接
填写 `reports/ENG-19/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
