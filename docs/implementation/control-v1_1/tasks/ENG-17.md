# ENG-17 · 迁移、归档、删除、恢复的兼容性闭环

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-02, ENG-05, ENG-09, ENG-10, ENG-11, ENG-14。

## 输入与实际代码入口
- `db/schema.sql`
- `db/db.go`
- `db/task_archives.go`
- `db/task_archives_restore.go`
- `db/tasks.go`
- `evidence/store.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 幂等加法迁移与旧新版本读取测试
- 归档格式增量及安全恢复规则
- 存量任务排空/升级手册

## 实现步骤
1. 遵循现有 schemaSQL/advisory lock 机制，运行重复迁移与旧快照迁移；不得 DROP 重建研究数据来修测试。
2. 原文/观测/evaluation 已有 append-only 触发器；删除采用既有归档/软删与保留策略，禁止随意取消触发器。
3. 归档绑定 contract、context、预算、执行、评估与计划引用；不要导出真实 secret 或可复用 grant。
4. 恢复到新 task identity 时重映射引用并标 restored；历史许可失效、运行中动作不得自动重新入队，重新执行需要新批准。
5. 对删除中执行与迟到结果做竞态测试；预算/证据审计记录按既定保留规则处理。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlMigrationTwice` | `db` | 迁移重复应用不丢数据、不改已批准语义。 |
| `TestControlArchiveRoundTrip` | `db` | 新增状态与证据索引可完整归档读取。 |
| `TestControlRestoreDoesNotReplay` | `db` | 恢复不重用授权/许可、不重新执行历史 queued/running。 |
| `TestControlLegacyNotPromoted` | `db` | 旧 scope 与事实不会自动提升授权或 confirmed。 |
| `TestControlDeleteWithLateResult` | `db` | 删除/关闭后迟到结果不复活任务或更新正式结论。 |

本票用例清单：`acceptance/ENG-17.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
只回滚应用读路径到兼容版本，不破坏性撤表；无法兼容时保持停机/只读并人工处理。

## 完成证据与交接
填写 `reports/ENG-17/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
