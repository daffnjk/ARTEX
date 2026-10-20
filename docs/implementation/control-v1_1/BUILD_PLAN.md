# ARTEX 可执行建设方案 v1.1

日期：2026-10-19。交付对象：在本地 ARTEX 仓库内工作的编码智能体、独立审查者和授权维护者。

## 0. 文件性质、依据和边界

这是对《ARTEX 总体建设方案 v1.0》的实施细化，不是新的总体架构，也不是已完成的代码。保留三环、确定性底座、证据驱动决策和现有 Planner/Worker 主干。新增的任务编号、接口、数据契约与测试名是本次建设建议；不是仓库已经具备的能力。

源码依据是用户上传的 ARTEX-main.zip，SHA256 为 `85877c685905d5766237f25e1ac718c46259a1e93b2440c4f59bf9c4884e4e54`。本次只静态读取源码，没有改动 ARTEX、运行其模型或联网测试。工作包中的工具脚本单独做了单元测试，见 audit/；它们的 PASS 不代表 ARTEX 的 PASS。实际代码改变后，旧行号与哈希不能继续当现状。

前提仍是明确授权。首版建成受控的本地 Web/API 验证闭环，只用合成业务数据和受控测试身份。不引入攻击载荷、批量凭据尝试、持久化、隐蔽执行、横向扩展或真实敏感数据搬运。未经单独验收的 Shell、浏览器、远程 MCP、任意脚本通道，在新自主模式中关闭。

## 1. 交付形态与使用方法

将整个 `ARTEX_build_packet_v1_1` 目录置于仓库 `docs/implementation/control-v1_1/`。不得覆盖现有 AGENTS.md、docs/autopilot/STATE.json 或用户改动。若目录已经存在，先比较哈希再决定版本，不整目录强制覆盖。

核心文件：

| 文件 | 用途 |
|---|---|
| AGENT_START.md | 可直接交给编码智能体的启动指令 |
| BUILD_PLAN.md | 实施技术规格及门禁 |
| tasks.json + tasks/ENG-00…19.md | 20 个有依赖的工程工单，步骤、代码入口和验收 |
| acceptance/ENG-xx.json、all.json | 84 条建议新增的必测名称；不是已存在测试清单 |
| contracts/ | 类型、状态、数据库事务和接口细节 |
| progress.json | 可恢复工作日志，初始全部 pending |
| templates/ | 每工单收据、只读审查和会话交接模板 |
| source/ | 已读源码锚点、哈希和静态复核边界 |
| scripts/ | 只读预检、任务选择、包校验、Go JSON 日志门禁 |
| audit/ | 本次交付包脚本测试结果，不包含 ARTEX 实施结果 |

包校验：`python3 docs/implementation/control-v1_1/scripts/verify_packet.py`。
选择下一票：`python3 docs/implementation/control-v1_1/scripts/next_task.py`。它只读取进度，不执行开发任务。
预检：`python3 docs/implementation/control-v1_1/scripts/preflight.py --repo . --output docs/implementation/control-v1_1/reports/preflight.json`。

Python 工具需要 Python 3.10+ 标准库；不安装依赖，不联网、不调用数据库、不运行仓库脚本。preflight 仅调用本地 go version 与 Git 元数据。METADATA_OK 不是构建或数据库通过。

## 2. 开工前必须固定的决策

### 2.1 保留并复用

当前 go.mod 声明 `go 1.26.3`，数据库是 PostgreSQL + database/sql 的 pgx driver，schema 通过 db/schema.sql 嵌入并在 db.Open 中幂等应用。当前存在 task_plans 的 ID、RowID、Revision/CAS，harness 的宿主观测/评估存储，以及 CompleteTaskGuarded 统一完成入口。不得另造同义模块。

本次环境只有 Go 1.23.2，未尝试下载新工具链。仓库的 scripts/test-harness-offline.sh 明确只把标准库 harness/CLI 复制到 Go1.23 临时模块测试；这不是整仓兼容性的证据。见 source/code-check.md。Go 的 go 行是最低工具链要求；执行者应使用满足 go.mod 的工具链，而不是修改要求使机器勉强运行。[R1]

### 2.2 三种任务模式

- direct：交付用户指定的一项结果，不扩展为全局覆盖。
- goal：对明确问题给出受证据支持的回答或未知。
- coverage：处理冻结覆盖矩阵，未测、阻塞和无效不算阴性。

三个维度独立：运行是否结束、评估是否完整、清理是否确认。禁止把“所有未知问题清零”作为统一终止门。

### 2.3 工程形态

保持模块化单体。建议新增纯领域包 control/ 和执行端口包 execution/，不是两套微服务。control 不导入 db/server；execution 只依赖 control 和端口接口；db 实现存储端口；server 做装配。harness 仍负责版本化评价逻辑，避免引入循环依赖。

首版不加图数据库、向量库、消息中间件或完整 POMDP 求解器。出站队列、租约和恢复先用 PostgreSQL 实现；评分先影子运行。

## 3. 五个可验收里程碑

| 里程碑 | 工单 | 可以声称的结果 | 不能声称的结果 |
|---|---|---|---|
| G0 规格/环境可复核 | ENG-00～01 | 本地状态、工具链、调用链与验收可检查 | 系统安全或性能已经改善 |
| G1 受控动作贯通 | ENG-02～07 | 真实 Worker 的已批准本地 HTTP 动作受合同与预算约束 | 任意 Shell/浏览器/MCP 都受控 |
| G2 证据和恢复贯通 | ENG-08～10 | 等待/取消/恢复正确，观测通过验证后更新结论 | 所有业务漏洞都可确定性判定 |
| G3 自主闭环可结案 | ENG-11～15 | 依赖、有界重规划、合理复测、独立评议、部分结果 | 策略已最优或全部未知清零 |
| G4 工程交付和审批 | ENG-16～19 | 操作面、迁移、故障矩阵、正式审查和有限开放清单 | 自动扩大到真实公网或上线生产 |

阶段从功能链逐步延长：ENG-07 即有真实 Worker→受控 HTTP；ENG-10 接入宿主观测→正式评价；ENG-14 才验证包含结案的完整生命周期。不得到最后才第一次接线。

## 4. 工单依赖与协作边界

默认按 ENG-00 到 ENG-19 顺序执行。依赖详见 tasks.json，不按一个工单一个长期常驻 Agent 配置资源。

完成 ENG-01 后，范围纯函数设计、预算测试和观测契约可以分工；但 db/schema.sql、共享身份类型、server/engine.go、agent/worker.go 同时只允许一个实现者写。使用独立 worktree 和独立测试 DB；多个 worktree 不共用会删除数据的测试库。

默认三种职责：协调/集成、实现、只读审查。只有平台真正支持受隔离的子会话/权限时才使用子智能体。不存在该能力时，不虚构“Reviewer 已批准”，状态写 needs_independent_review；可以继续不依赖该批准的任务，不跨过发布门禁。

## 5. 数据与身份契约

### 5.1 ID 与权限不能由模型自报

所有任务工具从认证上下文获取 task_id。通过 tasks.exploration_id 获取探索 ID；一对一关系不等于数值相等。实验、事实、计划和身份引用必须在同一允许上下文中。

ActionProposal 只含提出的目标、方法、参数、前提、预期与幂等键。禁止其包含批准人、执行许可、可信来源、实际成本或已确认结论。未知 JSON 字段和超界内容拒绝，而非忽略。

### 5.2 最小数据增量

建议新增合同、预算账户/预留、执行/尝试、outbox；扩展既有 tasks、task_plans、harness_observations/evaluations。具体字段、唯一键、锁序见 contracts/STORAGE_AND_TRANSACTIONS.md。

DB 的 BIGINT 任务/探索 ID 保持现状；新执行与请求 ID 由宿主生成。计划步骤 ID 仍由宿主分配。新增类型不能导致历史 API 中 ID 被混淆。

### 5.3 版本有不同语义

contract_version 表示授权正文版本；control_epoch 用于撤销/关闭/fencing；context_version 描述与实验有关的身份、对象和部署前提；criterion_version 固定判定方法；plan RowID+Revision 管并发便签。不要把它们合并成一个“每次写图就自增”的版本，否则正常新证据会使所有动作失效，也给去重留下逃口。

## 6. 准入与实际执行

### 6.1 确定性准入协议

宿主规范化动作、从数据库读取授权和方法能力，在事务内检查任务仍允许新工作、批准未失效、前提有效、预算足够。成功时同时写预算预留、execution、grant 摘要和 outbox；失败全回滚。事务提交前不访问目标。

同幂等键同摘要返回原执行；同键不同摘要返回冲突。幂等重放只能读取原状态，不会使旧许可重新有效。派发前与开始外部连接前重新检查 epoch 与期限。

PostgreSQL 行锁与事务级 advisory lock 的作用域不同；采用统一锁序并测试竞争，而不是认为“用了锁”即可。[R2] 本方案首版选择单任务串行关键状态写入来降低证明复杂度，不为高并发提前拆散原子性。

### 6.2 首版 HTTP 适配器的能力边界

适配器只执行登记方法，限制参数、请求数、超时、响应字节、身份引用、目标与输出目的地。每跳重定向重新检查；对 resolver 返回结果检查后直接连接选定地址，避免检查后重新 DNS 解析；验证 Host/TLS 身份。未经批准的代理环境不能改变出口。

本地测试同时启动允许和拒绝服务器，通过后者真实计数为零验收。网络命中与日志拒绝必须一致。未审计进程不能直接联网；首版干脆不装配任意进程执行工具。此举不等价于已实现通用 OS sandbox。

撤销不能倒转已经发到目标的请求；应保证本地撤销线性化点之后不再授予新的请求开始许可，并按能力及时中断在途连接。此前已经获准开始或已经发出的请求可能产生迟到效果；不能保证目标在撤销后绝无迟到网络包。给出测试平台上的取消上界和未确认情况，不承诺绝对瞬时撤销。

### 6.3 预算的硬/软边界

请求次数、并发、响应上限、deadline 可以在本地精确控制。token/费用是否精确取决于 Provider 计量与输出上限；未知 usage 不能按零计费或自动退款。使用最坏可验证上界与保守预留；没有可靠上界的维度明确标 estimated，并保留硬请求/时间底线。

task_llmRuntime 外包装若不能看见内部 failover 尝试，就应在实际 Provider 请求边界追加控制；不要只给逻辑 LLM 调用扣一次。既有 llm_usage/llmrec 是事后计量，与准入互相对账但不混为一谈。

## 7. 持久执行和恢复

Intent、Experiment、Action、Attempt 分开记录。执行服务必须支持 Get、Wait、Cancel、Reconcile；Wait 到时不等于工具到时，不等于重新 Submit。

租约过期只说明持有者可能失联，不说明原进程已经停止。使用递增 epoch 拒绝旧 worker 的新请求与结果；对未知外部效果先对账。outbox 至少一次投递与本地唯一键不能保证外部效果恰好一次。Temporal 活动的幂等文档也指出“执行完成但回报丢失”可能导致重试。[R3]

首版故障夹具用本地合成计数器：动作增加一次；在回报前模拟进程故障；恢复只读查询计数，不重复增加。无可查询远端且非幂等的动作进入人工确认，不自动尝试。

取消分为 request、ack、actual stopped、cleanup；ack 不算结束。对失联或远端无回收能力的执行，预算与清理状态保持未知并安排有界对账。超期不能继续循环，必须移交。

## 8. 证据先于信念：关键事务

原始证据保存并校验可读→宿主写 Observation→验证器校验前提/内容→事务内重查版本→正式 Evaluation+结论+依赖+失效+事件共同提交。

现有 RecordExperimentOutcome 的字符串路径只保留明确 legacy/research 兼容，不能再写新模式正式结论。新增提交方法应接受受控验证结果而非模型随口的 observed 文本；内部 SQL tx helper 不能自行另开事务。

对支持/反驳的对象和条件明确记录。前提无效时 inconclusive；有效反常观测可以推动探索，不能统一扣分；支持一个假设不自动排除非互斥解释。断言不足时保留其他解释。

harness 已有只追加存储与 request_id 去重，应保留。内容哈希只证明保存一致性，不证明目标陈述真实，也不证明评估来自独立人。复杂业务推断的真实性依赖方法定义和审查流程，而非一个 tool_verified 标签。

正式依据发生改变时，所有相关图写路径与结案共同锁 tasks 行或使用等价版本协议；只给最后 Complete 加锁而让证据写入无序，仍会产生竞态。冻结验收引用缺失也必须失败，不能 JOIN 后掉行就当全部满足。

## 9. 计划、决策与负结果

现有 task_plans 的 RowID 和 Revision/CAS 不变；PatchPlanStep 根据 step_id 原位更新。旧 TodoWrite 不得把新依赖静默擦掉。显式 DAG 拒绝循环、缺失、跨任务、失效身份与部署前提。

候选选择先过滤、后评分。首版分数表示目标推进、区分能力、有效覆盖、成本、剩余风险与无效重复；所有权重有版本并从 shadow 起步。多个假设预测同一结果时不因数量多得高分。结果收取队列与新实验队列分开。

语义去重键与网络调用幂等键分开；合法复测有可检查原因，例如前提/部署改变、修复复测、批准的独立确认。随机 nonce、改写文案或无关全局版本更新不能解锁重复。历史阴性只在条件一致时影响优先级，不跨任务按技术栈永久取消漏洞类。

## 10. 结案、人工操作面与报告

复用 CompleteTaskGuarded 的事务主干。先将任务置 draining 并阻断新授予，处理在途与清理，最后写 closed 和结果。direct 不增补全局覆盖，goal 可回答未知，coverage 基于冻结矩阵。

旧 tasks.status 的兼容映射必须写在同一服务：完整评估映射 done；墙钟/预算时间导致终止映射 timeout；其余 partial/inconclusive/aborted 使用 failed 的旧壳，但新 UI/API 必须展示真实 outcome/reason，不能误称系统异常或完整成功。映射可在 ENG-01 根据真实客户端兼容约束修订，但不能由不同入口各做一套。

closed 不代表 cleanup confirmed。无法自动清理时写移交责任、待办和未确认资源；不出“全部清理完成”声明。无模型服务时仍由 DB 数据生成报告。

最小任务详情需要显示授权/版本、预算 consumed/reserved、执行状态、证据链、结果完整度、未决问题和清理。只新增必要信息与认证操作，不重做 UI 设计系统。批准/撤销/取消必须有角色和任务所有权检查。

## 11. 迁移与回滚

每票涉及 schema 就提供加法迁移和兼容读；ENG-17 再做综合归档/恢复。db/schema.sql 的新增 FK 在依赖表定义后添加，并在旧库与重复执行中验证。沿现有迁移锁机制，不引入第二套迁移工具。

旧覆盖不是新授权；旧 fact 缺乏验证信息保持 legacy/unknown；新运行任务不得混用旧未受控执行器。部署前排空旧任务或明确隔离。恢复归档不复用 grant、不自动续跑旧 queued/running，不从归档导出密钥。

评分和摘要可以用 feature flag 回滚；授权、预算、证据血缘和实际执行约束不能回滚成允许。不兼容时停止新任务、对账在途、只读保留数据。

## 12. 智能体逐票执行规程

每票流程：定位现有实现→确认最小差异→写失败测试→实现→跑受影响回归/新增测试→保存原始日志→只读审查→修复→更新交接。

状态：pending → in_progress → needs_review → done_verified；任何外部依赖缺失可以进入 blocked。进度文件只是工作记录，不能成为信任根。done_verified 至少需要代码树/提交、测试原始日志、必测检查、迁移/兼容说明与独立审查引用。

同一根因最多 3 次修复循环仍失败就停止本票并交接；不删断言、不改成 Skip、不自动提高预算或打开外网。耗尽会话前写 HANDOFF，下一会话先验证代码与测试产物没有漂移再恢复，不凭记忆继续。

不得 `git reset --hard`、删除用户改动、覆盖 .env、读取并打印生产密钥、自动 push 或生产部署。依赖安装与镜像获取仅使用用户已批准渠道和锁定版本，不为图方便执行未知远程脚本。

## 13. 测试执行命令与证据口径

先运行 preflight，缺少合适 Go/隔离 PG 即 BLOCKED_ENV。注意普通 db/agent/server TestMain 可能读取 ARTEX_PG_DSN 或配置，因此必须在启动 go test 前将 ARTEX_PG_DSN 和 ARTEX_TEST_PG_DSN 明确设置为同一专用本地测试库。

测试库示例名称为 `artex_research_test_control_<unique_run>`；使用 127.0.0.1 明确端口和普通 URL，不带 query overrides，不把完整 DSN 打到日志。DSN 名字/localhost 不是隔离证明：还要检查它是明确创建的可丢弃实例、账号权限与原有业务数据库不同。必要时使用单独本地数据库容器；不得采用生产密码或生产 volume。

环境通过后，现有回归入口是：

```sh
# 在已经激活合适 Go、审定隔离 DSN 的终端执行；不是一键准备环境脚本。
export GOFLAGS=-mod=readonly
export ARTEX_PG_DSN="$ARTEX_TEST_PG_DSN"
go build ./...
go vet ./...
sh scripts/test-research-pg.sh
```

现有 runner 只选既有名称，不会自动跑完 84 条新增 TestControl。为每票另外执行所列包；下面以 ENG-02 的 db/server 为例（Linux Bash，pipefail 保留原退出码）：

```bash
set -uo pipefail
PACKET=docs/implementation/control-v1_1
mkdir -p "$PACKET/reports/ENG-02"
rc=0
go test -json -race -count=1 -run '^TestControl' ./db ./server \
  > "$PACKET/reports/ENG-02/go-test.jsonl" \
  2> "$PACKET/reports/ENG-02/go-test.stderr" || rc=$?
python3 "$PACKET/scripts/check_go_test_json.py" \
  --log "$PACKET/reports/ENG-02/go-test.jsonl" \
  --required "$PACKET/acceptance/ENG-02.json" \
  --exit-code "$rc" \
  > "$PACKET/reports/ENG-02/test-gate.json"
gate_rc=$?
cat "$PACKET/reports/ENG-02/test-gate.json"
if [ "$rc" -ne 0 ] || [ "$gate_rc" -ne 0 ]; then
  exit 1
fi
```

全部工单完成后，在已验证隔离环境运行新矩阵的 `go test -json -race -count=1 -run '^TestControl' ./control ./execution ./db ./agent ./server` 并对 acceptance/all.json 做同样检查；再跑既有全面回归。任何必测根用例/其子用例 skip、缺席、package fail 或命令失败都不是 PASS。已有测试中的可选真实模型项需要独立标注，不使它们悄悄变成通过。

测试名称是推荐契约；实现发生合理搬包/重命名时要同步清单且保留原断言、经审查批准，不能为了通过检查删掉项目。日志脚本只验证提供的日志，不证明日志真实性；正式收据来自用户控制的 CI/执行器。不得由实现智能体自行伪造 jsonl。

## 14. 最终验收矩阵

84 条新增测试分散在 20 票中，覆盖授权撤销、HTTP 出口、预算并发、原子准入、未知副作用、身份失效、可信采集、原子评估、计划依赖、重复、结案、审查、状态操作面、归档与完整闭环。

另外要求 6 类集成证据：真实 Worker 本地网络计数；隔离 PG 事务/迁移/恢复；取消进程/连接的真实确认；前端 check/build 与关键状态截图；安全边界独立审查；真实模型在获准本地任务中的行为评测。后两项缺失必须显式写 NOT_RUN 或 READY_FOR_APPROVAL，不自动上线。

首次生产可用结论只针对“测试过的适配器、方法、目标和模型配置”。未来扩展 browser、shell、MCP 或新业务问题族，需要各自的入口审计、取消/证据协议和网络隔离验收。

## 15. 本次交付的实际完成情况

本次完成了静态源码复核、20 票/84 用例依赖规格、数据与执行协议、启动/审查/交接模板，以及 4 个小型 Python 辅助脚本。scripts/test_packet_tools.py 的 19 项用例验证日志门禁与版本解析；包校验另外检查依赖图、验收清单和文件哈希。

本次未修改 ARTEX 生产代码、未编译整仓、未连接 PG、未运行真实模型、未部署目标。全部实施工单保持 pending。本环境预检为 BLOCKED_TOOLCHAIN；这份如实的阻塞记录用于提醒下一执行者建立自己的环境基线，不妨碍方案在合适环境落地。

## 参考与出处

本地源码锚点见 source/code-check.md、source/baseline.json。上一版方案及代码复核见原对话上传文件，不重复打包源代码或凭据。

R1：Go 官方 Go Toolchains，关于 go.mod 最低工具链和 GOTOOLCHAIN；2026-10-19 核对。`https://go.dev/doc/toolchain`

R2：PostgreSQL 官方 Explicit Locking，关于行锁、事务和 advisory lock；2026-10-19 核对。`https://www.postgresql.org/docs/current/explicit-locking.html`

R3：Temporal 官方 Activity Definition，关于外部副作用的幂等与重试边界；2026-10-19 核对。`https://docs.temporal.io/activity-definition`

上述资料用于校验工程语义，不证明本方案尚未运行的安全性、性能或业务收益。
