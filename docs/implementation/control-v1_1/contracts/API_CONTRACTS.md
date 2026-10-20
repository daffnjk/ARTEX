# 调用与错误契约（建议，不是已有 API）

## 1. 领域服务端口

| 操作 | 调用者 | 输入 | 成功/等待 | 必须拒绝 |
|---|---|---|---|---|
| ApproveContract | 已认证授权管理员 | task、expected revision、正文与标准版本 | 新合同版本与 digest | 模型身份、越角色、旧 revision |
| RevokeContract | 已认证授权管理员 | task、expected revision、reason | 新 epoch、受影响在途列表 | Agent 自报 actor |
| AdmitAndReserve | 宿主调度器 | TaskContext + ActionProposal | 新 grant 或原 execution 状态 | 无合同、前提不符、预算不足、参数变更 |
| GetExecution | 有该任务读权者 | execution_id | 状态与版本 | 跨任务读取 |
| WaitExecution | Worker/调度器 | 原 execution_id + wait_deadline | 原执行新状态或 WAIT_EXPIRED | 因等待超时自动产生新 execution |
| CancelExecution | 该任务授权操作人/监督器 | execution_id + reason | cancel_requested；后续报告实际停止 | 收到 ack 就宣告清理完成 |
| ReconcileExecution | 宿主监督器 | execution/attempt + lease epoch | 已知效果或 unknown/人工移交 | 无对账依据自行重试 |
| StoreObservation | 可信采集器 | 由宿主绑定血缘的原文/摘要 | 追加观测引用 | 模型直接写可信字段 |
| CommitValidatedOutcome | 验证服务 | 冻结实验、证据引用、版本化评估 | 原子结果与 state revision | 来源缺失、前提失效却支持结论 |
| PatchPlanStep | Planner 经宿主 | RowID、Revision、step_id、patch | 原 ID、新 revision | 旧 CAS、依赖循环/过期 |
| TryCloseTask | Planner 提议/宿主请求 | task、requested reason | 进入 draining 或 closed、outcome | 模型直接写 tasks.status |

管理 HTTP 路由名称在 ENG-00 核查现有路由之后确定；不要把上述内部操作名直接暴露成无鉴权 REST API。服务身份从 context 注入，不能由请求 JSON 的角色字段指定。

## 2. 错误分类

- CONTRACT_MISSING / CONTRACT_EXPIRED / CONTRACT_REVOKED：停止该路径，等待合法授权；不能自动扩大。
- SCOPE_DENIED / METHOD_UNSUPPORTED / UNBOUNDED_EFFECT：拒绝动作；调整候选不等于寻找绕路执行。
- APPROVAL_REQUIRED：等待已认证的人类批准；审批绑定摘要/版本/期限。
- BUDGET_EXHAUSTED：停止新探索，进入有限收尾。
- PRECONDITION_INVALID：实验不可判定，先修复/重验前提。
- EVIDENCE_UNAVAILABLE / EVIDENCE_CONFLICT / LINEAGE_MISMATCH：不更新正式结论。
- STALE_CONTEXT / PLAN_CONFLICT / STALE_LEASE：重读权威状态；不得 last-write-wins。
- IDEMPOTENCY_CONFLICT：同 key 异请求，拒绝；不自动换 key 重试。
- ALREADY_RUNNING / RESULT_AVAILABLE：返回原 execution/结果，而不是新派发。
- WAIT_EXPIRED：等待窗口到，不是执行失败。
- TOOL_UNAVAILABLE / OBSERVATION_INCOMPLETE：处理执行质量，不作安全阴性。
- TARGET_RATE_LIMITED：限速/冷却/停止，不引导规避。
- EFFECT_UNKNOWN / CLEANUP_UNCONFIRMED：有界对账或人工移交。

## 3. 最小真实 Worker 实验

固定本地合成场景：两个受控身份、几个合成对象、允许和禁止服务器、可变会话有效性、响应截断开关、计数型副作用测试端点。全部由夹具生成，无真实业务凭据。

正常链：合同批准→Planner 提出观测/实验→宿主授予→真实 Worker 受控 HTTP→宿主原始证据→类型化评估→信念/依赖更新→Planner 下一步→TryCloseTask。

异常链必须同样闭合：失效会话→inconclusive→重验前提；等待结束→收原结果；回报丢失→对账；预算耗尽→partial；撤权→停止新动作并清理。脚本化 Provider 用于可重复控制流，不将其结果包装成真实模型自主能力。

## 4. 可选行为评测的审批

真实模型试验必须单独记录端点、模型版本、方法版本、fixture seed、run budget、数据准入和授权人。模型只连接获准模型服务，工具只连接本地 fixture，评测不得借模型请求上传真实原始敏感证据。缺少批准/密钥时记 NOT_RUN，保留确定性集成证据但不宣称模型行为验证已完成。
