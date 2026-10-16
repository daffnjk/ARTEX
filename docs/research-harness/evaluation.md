# M1a 实施与验证记录

日期：2026-10-16。基线：`e6ec56999175509551a1e43a3ee3b053d0941bd9`。
本记录只描述本次实际执行的检查，不代表整个 ARTEX 已通过验证。

## 已交付

- 零第三方依赖的 `harness` 证据验收内核、三个版本化契约。
- 任务/实验/上下文绑定、原始字节 SHA-256、采集完整性和容量检查。
- 程序验收器注册表；没有规则时保留 unknown/inconclusive/review。
- 只读本地夹具 CLI；不执行命令、不连接目标、不写任务状态。
- 可选 `ARTEX_GOAL_COMPLETION_MODE=review` 工具包装器，接入已有 `AugmentTools`。
- 单元测试、离线测试脚本、项目工具链下的集成测试定义、分阶段路线和回滚说明。

## 实际本地验证

运行环境：Linux amd64，Go 1.23.2。仓库基线要求 Go 1.26.3。
本地直接克隆/依赖下载不可用，源码通过 GitHub 连接读取；没有运行完整 ARTEX。
修改前 `agent/assembly.go` 的本地文本已按 Git blob SHA 校对，等于源仓库的
`d28f207f3151b9dd59cbeae453642b6ee5ac0a27`，实际运行路径只改一行包装调用。

执行：

```sh
sh scripts/test-harness-offline.sh
```

脚本在临时 Go 1.23 module 内测试新内核与 CLI；未改根 go.mod，也未用仿造 SDK 替代真实依赖。

| 检查 | 本地结果 |
| --- | --- |
| 新内核与 CLI 单元测试 | PASS：15 个顶层测试，含命名子测试共 50 项 |
| `go test -race`（仅新内核/CLI） | PASS |
| `go vet`（仅新内核/CLI） | PASS |
| CLI 构建与本地夹具运行 | PASS，预期退出码 2 |
| `harness` 语句覆盖率 | 99.1% |
| `cmd/harness-eval` 语句覆盖率 | 75.0% |
| 项目 Go 1.26.3 下的编译/Agent 集成测试 | 本地未运行；需实际 CI/对应工具链验证 |
| PostgreSQL、UI、实际目标测试 | 未运行，且不属于本次离线内核验收 |

覆盖率不表示安全证明。CLI 中未执行的入口/未来结果分支不能被算成已验证。
完整夹具的实际结果要点：

```json
{
  "validity": "unknown",
  "conclusion": "inconclusive",
  "disposition": "review",
  "issues": [{"code": "criterion_unimplemented"}]
}
```

证据完整只能证明材料与引用一致；没有可信领域验收器时不能确认技术命题。

## 重点回归样本

不支持的契约版本、缺失/重复引用、无效哈希、跨任务/跨实验、过期上下文、缺失执行/身份/对象/时间、截断与超大材料、哈希不符、未知验收器版本、伪造验收引用、无证据支持、无效实验上的支持/反对、验收器错误/异常、取消、重复夹具 ID 与多余 JSON 输入均有测试。

`agent/completion_guard_test.go` 还包含模型参数试图声明已批准、legacy 行为、无关工具、DB 解析后包装与禁用工具不重引入的集成测试；**这些 Agent 测试不能因源代码已写好就标为本地 PASS**。

## 已知限制与下一合并门槛

当前内核还没有可信数据库 Resolver、领域验收器、评估持久化、共同数据库完成门、人工批准 UI、任务计划恢复或动态信念更新。复核模式只保护两个经公共装配路径暴露的完成工具；直接存储写入与其他旁路仍需 M1b 审计。默认 legacy 是兼容性选择，不提供新增完成保护。

GitHub Actions 工作流定义可检查新内核及 Agent 适配器；提交本记录时不预先宣称 CI 成功。PR 合并前应核对实际运行结论，并在本地隔离实例完成 legacy/review 两种模式烟雾检查。
