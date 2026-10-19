# M0.1 — 场景/真值登记册与可执行回放

研究分支 `research/autopilot-v2`。登记位置：`harness/scenarios/*.json`（16 份，
按 id+version 不可变标识），加载与回放实现：`harness/scenarios.go`。

## 回放命令

```sh
# 登记册加载校验 + 全场景回放 + 清单输出（stdlib-only，可离线）
go test -race -count=1 -run 'TestScenario' -v ./harness

# 离线等价（隔离 Go 1.23 模块、GOPROXY=off，与 CI offline-core job 同路径）
sh scripts/test-harness-offline.sh
```

每步回放走**与生产同一套评估内核**（M1a.1 可信绑定路径：
`NewEvaluatorWithAcceptance` + 冻结验收源 + 宿主注册的确定性验收器），断言
实际 `conclusion/disposition/首个问题码` 与场景真值一致。

## 样本清单（类 ↔ 场景 ↔ 真值要点）

| 类 | 场景 id | 真值（结论/处置/问题码） |
|---|---|---|
| valid-session | valid-session | supported / confirm（唯一确认路径） |
| invalid-value | invalid-value | refuted / not_supported（仅本命题，非"目标安全"） |
| missing-evidence | missing-evidence | inconclusive / review / evidence_unavailable |
| contradictory-observation | contradictory-observation | inconclusive / review（验收器不得择优） |
| condition-version-change | condition-version-change | inconclusive / **retry** / stale_context |
| tool-error-or-cancel | capture-incomplete；cancelled-context | review / capture_incomplete；review / cancelled |
| duplicate-observation | duplicate-observation | review / invalid_request（请求级拒绝） |
| oversized-output | oversized-output | review / artifact_too_large（>1 MiB 合成材料） |
| cross-task-reference | cross-task-reference | review / evidence_binding_mismatch |
| acceptance-binding-mismatch | acceptance-binding-mismatch | review / acceptance_binding_mismatch（改命题 fail-closed） |
| tampered-evidence | tampered-evidence | review / digest_mismatch |
| verifier-indefinite | verifier-indefinite | inconclusive / review（无问题码，验收器明示不能判定） |
| verifier-failure | verifier-error；verifier-bad-verdict | review / verifier_error；review / invalid_verdict |
| restart-resume | restart-resume（2 步） | 步1 review/capture_incomplete → 步2 supported/confirm（重启后凭权威材料恢复） |

每份场景同时登记：`allowed_actions`（评估类场景恒为 evaluate_only）、
`stop_conditions`、`controls`（model/tool_budget/target——离线场景为
none-required/none/none，M0.2 预注册将扩展同一格式引入真实模型与 token 预算）。

## 真值来源与标签准则

- 真值是**构造性**的：材料字节由作者控制，预期判定按评估内核契约
  （`harness/evaluation.go` 的路径映射）写定——不是模型自评，也不是事后
  挑选的"通过"标签。
- 唯一 confirm 路径 = 完整材料 + 摘要一致 + 绑定匹配冻结验收 + 可信验收器
  supported；其余一切路径 fail-closed 为 review（或 invalid→retry）。
- `not_supported` 只否定该命题；`retry` 只是建议，绝不自动重放外部动作。
- 场景文件不可变：修订场景 = 新 version 文件，旧文件保留（M0.2 对照与 M4
  消融需要可追溯的版本化基线）。

## 未覆盖范围（如实）

- 无真实模型参与：本登记册是评估内核行为基线；M0.2 的固定基线运行需要
  模型凭据与预算（STATE: MODEL-BUDGET NEEDS_INPUT），届时在同一登记格式上
  预注册运行配置。
- 无真实目标/网络捕获：观察材料是构造的；可信采集通道属 M2。
- 验收器族是确定性仿真（payload-equals/contains、unknown、error、bad-verdict），
  领域验收器属 M2。
- acceptance-binding-mismatch 仅覆盖"改命题"变体；错 criterion/版本/实验/
  上下文走同一 `spec.matches` 路径（代码级同一判定），登记变体待 M2 补充。
- 多目标/并发任务/跨实验条件失效场景留给 M3 条件化信念阶段扩展。
