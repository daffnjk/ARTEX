# ARTEX 自主建设流程覆盖包 v1.2

这是对已存在的 v1.1 工程包的增量流程覆盖，不是新的一套工程工单，也不是 ARTEX 业务补丁。

将本目录 `autonomy-v1_2` 放入：

    ARTEX/docs/implementation/control-v1_1/autonomy-v1_2/

保留原 tasks.json、acceptance、contracts、脚本、进度历史和报告，不整目录覆盖。

把 AUTONOMOUS_EXECUTION.md 作为维护者新指令交给本地编码智能体。该指令允许在既定工程范围内自动开发、本地测试和提交，在全部通过后推送专用交付分支；不允许生产部署或放松目标授权边界。

## 调度器

旧 next_task.py 只认可 done_verified。本包的只读调度器增加 verified_local，不把独立审查是否完成作为开发依赖条件。

```bash
# 在 ARTEX 仓库根目录执行
python3 docs/implementation/control-v1_1/autonomy-v1_2/scripts/next_task_autonomous.py

# 只测试辅助调度器；不运行 ARTEX 或数据库
python3 -m unittest discover \
  -s docs/implementation/control-v1_1/autonomy-v1_2/tests -v
```

调度器不会写 PASS、改进度、调用模型、运行测试、提交 Git 或自动续跑。编码智能体按真实测试证据写入 verified_local，然后重新选择下一票。阻断测试不被忽略。

## 已执行与未执行

本包的 20 项调度器单测已在交付环境运行；另用原 v1.1 的真实任务图进行只读兼容性模拟。见 audit/。

这些结果不代表 ENG-01～19 已实现，也不代表用户 Mac、数据库、远端 Git 或 CI 已被访问。没有修改用户实际工作区，没有推送任何仓库。

## 主要文件

- AUTONOMOUS_EXECUTION.md：完整一次授权与持续执行指令。
- scripts/next_task_autonomous.py：只读任务选择器，Python 3.10+ 标准库。
- tests/test_next_task_autonomous.py：辅助调度器测试。
- audit/：本次脚本测试和兼容性模拟收据。
- manifest.sha256.json：本覆盖包文件内容摘要，非身份认证或业务验收。

原 v1.1 的 20 工单和 84 条验收要求保持不变；只是将逐票人工审查推迟为交付后的集中审查，并增加授权的最终 Git 同步。
