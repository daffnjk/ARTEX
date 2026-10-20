# 存储、锁序与迁移契约

状态：建议实现规格。必须复用现有同义表；以下不是可直接执行的 SQL 迁移。ENG-01 完成字段对照，后续沿 db/schema.sql 幂等迁移方式补充。现有 tasks 与 explorations 为 1:1，但 ID 数值不保证相等。

## 1. 最小新增与扩展

| 对象 | 字段建议 | 必须的约束 |
|---|---|---|
| task_control_contracts（新增） | task_id BIGINT、version BIGINT、正文 JSONB、digest、approved_actor、approved_at、expires_at | PK(task_id,version)；审批正文不可变；actor 来自认证宿主 |
| contract_events（可复用既有审计表） | task_id/version、event/reason、actor、event_time | 追加审计；撤销不重写旧授权正文 |
| tasks（扩展） | control_mode、contract_version、control_epoch、control_phase、outcome、stop_reason、cleanup_status | 旧任务 mode=legacy；新 mode 必须合同；phase accepting/draining/closed；由完成服务统一写 |
| task_budget_accounts（新增） | task_id、dimension、limit_units、used_units、reserved_units、close_reserve_units、measurement_kind | PK(task_id,dimension)；整数非负；硬额度不被普通探索用掉；estimated 与 exact 区分 |
| task_budget_reservations（新增） | reservation_id、task_id、dimension、execution/call/attempt_ref、request_key、reserved、actual、settlement_state | 任务内幂等唯一键；实际未知不释放；结算事件幂等；归因不空缺 |
| action_executions（新增） | id、task_id、exploration_id、intent/experiment/step_ref、action_json、digest、idempotency_key、semantic_key、contract_version、control_epoch、grant_expires、state、effect_status、cleanup_status | UNIQUE(task_id,idempotency_key)；同键异摘要冲突；grant 不可由 Agent 写；跨归属引用拒绝 |
| execution_attempts（新增） | id、execution_id、attempt_no、lease_epoch、lease_owner、lease_expires、started/finished、result_kind、effect/cleanup | UNIQUE(execution_id,attempt_no)；旧 epoch 不能写当前状态；未知不回 queued |
| control_outbox（新增） | id、task_id、aggregate_id、event_type、event_version、payload、delivered_at | UNIQUE(aggregate_id,event_type,event_version)；与领域写入同事务；消费者可重复收到 |
| task_plans（复用） | 原 RowID/Revision/steps JSON；在 PlanStep 中补 requires_step_ids、requires_claim_refs、method/context version | ID patch 保身份；旧整表更新不能丢依赖；DAG 有界 |
| harness_observations（复用） | 原 task/execution/context/payload/digest 等；必要时扩 attempt、source、validity、schema_version、raw_object_ref | 保留追加限制和容量上限；trusted 来源由采集身份生成 |
| harness_evaluations（复用） | 原 request/task/criterion/version/report 等；必要时追加 evaluator principal 与 attempt/context 引用 | 继续 request_id 幂等；与正式结论更新共用 tx helper |
| hypotheses/探索事实（复用） | 状态、来源、适用条件、支持与反证依赖 | 结论状态与 provenance 分开；正式依据撤回可以失效；历史未知不升级 |

新 execution 的 id 可用仓库已有 uuid 包；不要为了新表改变旧任务 ID 类型。审批动作和 secret 本体分离，库中只存 secret_ref 与允许使用范围。原文对象不存成模型可写路径，路径由受控存储解析。

## 2. 统一锁序

第一版用一条 tasks 行作为同任务关键 mutation 的串行化根：

`tasks → 当前合同/版本 → 预算账户（dimension 排序）→ execution → 实验/依据节点（ID 排序）`。

准入、撤权、结案、正式结论/依据失效、预算结算应遵守同序；兼容旧图写入口不能绕过关键版本协议。多任务操作按 task_id 升序；无法纳入锁序的旧入口在新模式中禁用或显式接桥。

不持有数据库事务等待 LLM、网络请求、人工审批或文件大对象上传。对象先 staging/校验 ready，事务只引用 ready 对象。提交前不要发目标请求，也不要只发内存通知；提交后的通知用于唤醒，outbox 用于恢复。

## 3. Tx A：AdmitAndReserve

1. 从认证上下文取得 task_id，规范化 ActionProposal，拒绝权威字段。
2. BEGIN；锁 tasks；查询 contract_version/control_epoch/control_phase。
3. 任务关闭/排空、合同缺失/过期、审批错误、方法未批准、参数越界 → ROLLBACK。
4. 查询相同幂等键：摘要同则返回原 execution 当前状态；摘要异则冲突。不能刷新旧许可。
5. 检查前提/依赖与版本；锁预算账户并预留最坏有界消耗；失败不留下任何部分预留。
6. 语义重复检查与唯一在途约束；确有复测理由才允许新的逻辑动作。
7. 插入 execution、许可摘要、预算预留、outbox。COMMIT。
8. dispatcher 获得提交后的事件，领取 attempt/lease；执行前再核验 epoch/期限。

“读取到授权”与“网络效果发生”不天然原子。运行时 supervisor 负责把请求开始许可与撤销放在可线性化的本地序列中。只能保证撤销线性化点后不批准新的请求开始；已获开始许可或已发出的在途请求需尽力取消，无法撤回远端已产生效果。不得承诺撤销之后目标绝不会收到迟到网络包。

## 4. Tx B：CommitValidatedOutcome

1. 宿主采集证据并校验 ready/digest；验证器根据冻结版本产生结构化判断。
2. BEGIN；锁 tasks，再锁实验/相关节点；重查 task/execution/attempt/context/criterion 血缘及当前前提。
3. 非有效前提只能 inconclusive；其他任务伪引用拒绝；原文失效或请求版本冲突不接受。
4. 幂等检查 evaluation request_id（由宿主创建）；同 ID 异内容冲突。
5. 同事务写 evaluation、假设/事实状态、支持与反证依赖、失效与 outbox。
6. 任一失败全部 ROLLBACK；提交后 Planner 才能读新有效状态。

已有 RecordEvaluation 若内部自行 Exec/开事务，应抽私有 `...Tx(*sql.Tx,...)` helper；不要依次调用多个独立提交的方法后称为原子操作。旧字符串 RecordExperimentOutcome 只留显式研究兼容，无法写新模式正式结果。

## 5. Tx C：TryCloseTask

先以 tasks 行锁将 accepting→draining，递增 control_epoch，拒绝新派发。提交后取消/查询在途，不在事务里等网络。需要收尾动作时只有宿主预定义 cleanup capabilities 可用，仍受剩余授权与收尾预算约束。

最后一笔事务重锁 tasks：

- 冻结验收引用必须完整、可读，缺失不因 JOIN 掉行而被忽略。
- 判断任务模式、证据有效性、未决项与覆盖版本；complete 要求当前契约被有效满足。
- 检查执行和清理；未知副作用写 outcome 限制与移交。
- 写 run/result/reason/cleanup 与旧 status 兼容值，仍由现有 CompleteTaskGuarded 的共同内核完成。
- 保留先到终态，迟到 Worker/Planner 不得把 timeout/revoked 改回 done。

## 6. 有限重试与预算原子性

数据库死锁/序列化错误可重试纯数据库事务（有界、保留同幂等键），不重放已发生的外部动作。lease 过期不会自动返还资源。重定向及底层重试属于实际请求，需要 request-level 额度；同一个 Action 的 redirect cap 必须包含在预留中。

计量有精确和估算两类：精确维度约束 used+reserved≤limit，估算维度若 Provider 返回超估消耗则如实登记 overrun 并停止扩展，不能改写实际账单满足不等式。Provider 无法可靠限制输出/未上报 usage 时，此维度不能宣称精确硬预算。

## 7. 迁移、删除与恢复

新增 FK 在被引用表出现后定义；追加表不提前置于 schema.sql 的 tasks 定义之前。复用启动迁移连接锁；旧库与重复迁移均实测。

现有 harness append-only 触发器不随意取消。归档/软删除依据既有保留机制；恢复不会刷新旧授权、续跑旧执行、复制可用 grant 或把未验证历史升级为新证据。恢复到新的 task ID 时重映射引用并要求新授权。
