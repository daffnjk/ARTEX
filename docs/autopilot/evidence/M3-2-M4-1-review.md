# M3.2–M4.1 独立审查记录（2026-10-18 深夜）

- 审查者：独立只读 agent（agent_068cba5d），范围 39cda65..ffe26f4
- 结论：**APPROVE_WITH_P1**（1×P1 + 7×P2）
- P1：RecordExperimentOutcome 将胜出假设置 supported 时未把区分性证据 fact
  并入 basis_node_ids → 失效监测按 basis 检索漏掉真正支撑证据的撤回路径
  （db/hypotheses.go:353/:412；空 basis 假设永久 supported 的口子）。
- P2 清单（修复中，逐项核实）：
  1. research-reviewer 清理结构性失效（触发器拦 DELETE，错误被丢弃，报告 kept_rows=false 不实）
  2. planner 推荐读取 recErr 静默丢弃（fail-open）+ DispatchExperiment 对 task_constraints 仅提示不强制
  3. review_packet 完整性扫描错误静默省略 + 多处 rows.Err() 未查
  4. notification.go false_positive 路径 node 扫描错误静默跳过失效
  5. hypotheses.go 失效事件插入未按 RowsAffected 门控（可伪造 from_status）
  6. proposal_mining Validate 失败静默丢弃 + rows.Err()
  7. 跨探索唤醒/级联删除失效缺口（心跳缓解，记录为已知边界）
- 审查确认健全面：false_positive 同 tx 原子性、精确匹配判定、追加式触发器齐备、
  干净上下文无提示泄漏、Notify 缓冲不 panic、无未信输入拼 SQL。
