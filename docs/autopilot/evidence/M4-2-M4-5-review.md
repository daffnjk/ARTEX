# M4.2–M4.5 批次独立审查记录（2026-10-19 凌晨）

- 审查者：独立只读 agent（agent_efbcfd31），范围 ffe26f4..052b238
- 结论：APPROVE_WITH_P1（1×P1 + 6×P2），全部修复随后提交
- P1：RecordExperimentOutcome 把模型上报的 factID 未经校验并入 basis_node_ids
  （无存在性/kind/血缘检查——跨探索嫁接与幻影 id 均可发生，恰好破坏该修复
  要维护的 basis 不变量）。
- P2：级联删除 DISABLE TRIGGER 与并发实验记录的死锁窗口（改 ACCESS EXCLUSIVE
  前置）；single 模式下变体 E 静默按 B 装配；hunt grounded 判定 Contains 口径
  可经 record_fact 伪造（改全等+yields 边）；清理错误仍被丢弃（改 cleanup_error
  上报）；profile 名硬编码（改透传）；dispatch deny 检查跨连接 check-then-act
  （改单事务）。
- 审查确认健全面：消融开关不可能泄漏生产（倒置零值+调用面 grep）；计量无旁路；
  hunt 环境与契约测试一致；DSN 守卫与凭据不打印；失效事件门控齐备。
