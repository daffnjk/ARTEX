# ENG-00 当前源码入口与验证范围

基线 HEAD eb872ee7ee0359fe6fae183773d4a7511f8e9de6。本轮只改测试与本地测试 runner；未启用自主控制模式。此清单定位代码，不能视为授权能力已验收。

## 装配与外部效果

| 路径 / 符号 | 实际职责与本票处理 |
|---|---|
| cmd/artex/main.go | 程序入口；本票不启动服务 |
| server/task_llm.go:564、574 | NewWorker / NewPlanner 组装；本票直接使用相同 NewPlanner 和 Plan 路径、内存 Provider |
| agent/planner.go:387 Plan | ToolSet→AugmentTools→SDK→Provider；默认认知三工具与信念渲染、显式纪律在真实请求中分别断言 |
| agent/worker.go:373–453 Execute | 基础工具、Bash、WebFetch、代理与增强工具；本票不调用 Worker 或这些出口 |
| agent/assembly.go:39 AugmentTools；server/assembly.go:26、239 | skill/MCP 增强及工具解析；测试无服务端回调、无 MCP 连接 |
| server/customtool.go:329–448 | command 复用 Bash、script 子进程、HTTP 自定义工具；本票不执行 |
| mcphttp/client.go:95、121、377、427、503 | HTTP/SSE 初始化、关闭和调用；本票不执行 |
| agent/side_questions.go:12 attachSideCapture；sidequestion/ | 后台回答 Provider/记录路径；本票不启动 |
| notify/http.go:142、154；selfupdate/github.go:89；selfupdate/stage.go:220 | 通知、版本查询、下载；本票不触发 |
| traffic/ | 记录代理与流量采集；本票不启动 |
| norma@v0.4.3 agentcore/tool/llm | 已有 SDK 的工具/Provider 出口；本票 Provider 仅记录请求，返回普通文本，不输出 tool_use |

HTTP、子进程及 socket 静态定位的原始检索保存在 effect-entrypoints.raw.txt。未对浏览器插件、外部 MCP 或后台通道作安全验收；首版相关门禁仍关闭，不据此声称未知环境安全。

## 正式证据与宿主映射

- db/harness_store.go：StoreHarnessObservation → Harness(taskID).Resolver → RecordEvaluation / LatestEvaluations。
- db/harness_acceptance.go：SetHarnessAcceptance / HarnessAcceptanceResolver；harness/acceptance.go、evaluation.go 进行冻结标准与血缘判定。
- db/hypotheses.go：UpsertHypotheses / DeclareExperiment / DispatchExperiment / RecordExperimentOutcome；旧研究字符串结果接口不等于新模式正式结论事务。
- 身份路径：db.GetTask(taskID).ExplorationID → db.Exploration(explorationID) → TaskID / Root / GetNode；新测试显式拒绝同值 ID 夹具，并验证误用 taskID 和其他任务探索均读不到该节点。未新增 ENG-02 授权系统。

## 终态写入

- server/engine.go:850、891 与 engine_timeout.go:218 → Manager.CompleteTaskGuarded → db/task_completion.go:34。
- 兼容入口仍存在：Manager.SetTaskStatusGuarded → db.SetTerminalStatusGuarded；Manager.SetTaskStatus → db.SetStatus（db/tasks.go:480、493）。本票只定位，未重构或声称所有入口已纳入新协议。

## 测试入口与门禁

- db/agent/server TestMain 会读取 db.DSN 并尝试 PostgreSQL advisory lock；新 runner 强制两种 DSN 相等且是本地专用库，禁止配置回退，先验证 DDL/DML 回滚能力。
- scripts/test-control-eng00.py：go test -json -race -count=1 -timeout=5m -run '^TestControl'，只使用 acceptance/ENG-00.json；另跑 scripts/control-eng00-regressions.json 中17项相关原有测试。
- 每轮目录独占创建，保存 stdout JSONL、stderr、原始退出码、日志 SHA256、源码摘要、环境目标与 required 清单；调用原包 check_go_test_json.validate，另有 CLI 独立重读校验。
- 现有 scripts/test-research-pg.sh 的静态过滤不覆盖新 TestControl，且部分文本 SKIP 检测不可靠；保留旧研究 runner，本票新增独立入口，不以其输出替代本票门禁。
- test-harness-offline.sh 是临时 Go1.23 子集；CI research-harness.yml 尚未接入此本地 ENG-00 runner。未运行或声称远端 CI / 全仓测试通过。
