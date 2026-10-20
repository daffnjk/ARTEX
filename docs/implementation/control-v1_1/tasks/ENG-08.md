# ENG-08 · 租约、取消和未知副作用恢复

状态：pending。本票是待实现规格，不是已完成声明。
依赖：ENG-05, ENG-07。

## 输入与实际代码入口
- `server/engine.go`
- `execution/（新增）`
- `db/schema.sql`
- `db/tasks.go`

“新增/建议”路径不是已有能力。开始前重新定位符号并读调用链；当前树不同先提交差异，不按旧行号机械修改。

## 必交付
- 持久租约与 fencing epoch
- Get/Wait/Cancel/Reconcile 协议
- 恢复扫描与资源回收监控

## 实现步骤
1. Wait 只等待原 execution，不触发新执行；worker lease 与 action grant 有不同有效期。
2. 以数据库 epoch fencing 拒绝旧 worker 结果及新动作；租约过期不等于原进程已停止。
3. 工具开始外部效果后丢失回报标 effect=unknown；非幂等/无法查询动作不得自动重试；本地 counter fixture 提供只读对账。
4. 取消先持久化 request，再由 supervisor 中断真实请求/受控进程；远程取消不可确认必须 unconfirmed。
5. 外部状态未知时预算不释放；重启对账有上限并将无法自动解决的情况交给人；不得把 outbox at-least-once 声称为外部 exactly-once。

## 验收用例（全部为待新增名称，不是已经运行的测试）
| 用例 | 包 | 核心断言 |
|---|---|---|
| `TestControlWaitTimeoutDoesNotResubmit` | `execution` | 多次 Wait 超时只对应原动作。 |
| `TestControlLeaseFencesStaleWorker` | `db` | 旧 epoch 的结果写入和新调用都被拒绝。 |
| `TestControlEffectThenCrashReconcile` | `server` | 本地计数效果已发生、回报前崩溃；恢复不重复增加。 |
| `TestControlCancelUnconfirmed` | `execution` | 没有实际停止确认不返回 cleanup=confirmed。 |
| `TestControlRecoveryKeepsBudget` | `db` | 重启与租约过期不清空已消耗/未知预留。 |

本票用例清单：`acceptance/ENG-08.json`。包名对应建议落点，若重定位需要带理由更新清单并由审查确认，不能删除验收语义。
验证方式：先验证隔离环境，再对所列包运行 go test -json -race -count=1 -run '^TestControl'；调用 scripts/check_go_test_json.py 检查 required 清单及原退出码。原有受影响回归另外运行，不能仅跑新增测试。

## 范围与设计备注
仅完成本票及必要最小兼容修改；不增加无关平台功能、依赖升级或攻击能力。

## 回退
暂停领取并保留 unknown/cleanup 状态；只能经对账解除，不批量重置为 queued。

## 完成证据与交接
填写 `reports/ENG-08/result.json`（参考 templates/task-result.example.json）、测试原始日志、diff 摘要、代码树 SHA、迁移结果和只读审查记录。外部阻塞写 BLOCKED，不是 PASS。最多三次修复同一根因后仍失败则交接，不删测试、不调低门槛。
实现者不能自行把自己的审查结果当独立批准。无独立审查工具时记录 needs_independent_review。
