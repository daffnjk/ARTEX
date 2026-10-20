# ARTEX 工程执行包 v1.1

这是一份可以交给编码智能体逐票实施的规格包，不是已经完成的系统补丁。

**入口：**将本目录完整放到 ARTEX 仓库的 `docs/implementation/control-v1_1/`，然后将 `AGENT_START.md` 内容交给拥有该仓库读写与测试权限的编码智能体。不要覆盖根 AGENTS.md 或原来的研究状态。

先读 `BUILD_PLAN.md`，再按 `tasks.json` 执行 `tasks/ENG-00.md` 起的 20 票。初始进度都为 pending。`acceptance/` 给出 84 条待实现测试，不代表仓库已有这些测试。

辅助脚本需要 Python 3.10+ 标准库：

```sh
python3 docs/implementation/control-v1_1/scripts/verify_packet.py
python3 docs/implementation/control-v1_1/scripts/preflight.py --repo .
python3 docs/implementation/control-v1_1/scripts/next_task.py
```

脚本仅辅助检查和选择，不替代 CI、审查、授权或编码 Agent；不会安装工具链、运行 ARTEX 或访问目标。首次 verify 校验分发包哈希。实施者若经审查修改计划，应保留原始包并生成新的规格版本，不能为消除差异偷偷更新 manifest。

`audit/` 保存本次包工具的 19 项测试及静态预检。ARTEX 整仓未编译/测试，当前作者环境 Go1.23.2 低于上传 go.mod 的1.26.3，预检记录为 BLOCKED_TOOLCHAIN。用户环境以 ENG-00 实查为准。

本包只面向已授权本地/隔离测试的控制与可靠性工程；不含漏洞利用载荷，不自动扫描外部目标，不自动 push 或上线。
