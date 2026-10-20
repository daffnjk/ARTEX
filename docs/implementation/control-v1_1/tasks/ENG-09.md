# ENG-09 · 接通可信原始证据与宿主观测

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-01, ENG-05, ENG-06。

## 输入与实际代码入口
- `evidence/store.go`
- `db/harness_store.go`
- `harness/evaluation.go`
- `db/schema.sql`
- `agent/hypothesis_tools.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- adapter→原文→StoreHarnessObservation 采集桥
- 来源/有效性/版本映射
- 原文限额、脱敏视图与幂等规则

## 实现步骤
1. 优先扩展已有 harness_observations/harness_evaluations；不要平行造同义 evidence2/evaluation2 库。
2. 保持 harness 当前小型 BYTEA payload 约定和上限；大响应复用 evidence 内容寻址存储，观测引用已确认可读对象。
3. 采集 ID、task/execution/attempt/context 由宿主填写；模型只能提交解析建议或观测引用，不可写可信来源。
4. 同 ID 同内容幂等；同 ID 不同摘要冲突；完整性缺失、截断、身份失效分别标记。
5. 写入原文完成后才允许正式评估；未引用对象安全 GC，不删除被验收依赖引用的原文；日志只保存脱敏摘要。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlObservationLineage` | `db` | 跨 task/exploration/execution/attempt 嫁接拒绝。 |
| `TestControlObservationIDConflict` | `db` | 同 ID 内容不一致拒绝且原文不变。 |
| `TestControlTruncatedObservationInvalid` | `execution` | 超过字节上限的观测不得作为完整证据。 |
| `TestControlModelCannotForgeObservation` | `server` | 模型工具传入伪采集记录无法进入可信表。 |

本票用例清单：`acceptance/ENG-09.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
保持 append-only 原文；停用新评估入口而非回落模型自报。

## 完成证据与交接
填写 `reports/ENG-09/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
