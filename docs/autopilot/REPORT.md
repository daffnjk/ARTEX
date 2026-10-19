# 证据驱动认知智能体——研究报告（research/autopilot-v2）

版本：2026-10-19 01:00（分支推送至 8cf159d；全部结论仅引用本分支可复现实验与证据文件）
定位：本研究分支的评测报告与结论综合。运行说明见 RUNBOOK.md；任务级规格见
task-specs/；原始证据见 evidence/（本报告中每个结论都给出指针）。

## 1. 研究问题与系统主张

在部分可观测环境（渗透测试是典型）中，像人类专家一样工作意味着：条件化观测 →
竞争假设 → 区分性实验选择 → 信念修订 → 错误恢复 → 可独立验收的结论。工程控制论
映射：观测=反馈信号、计划=控制律、假设集=环境内部模型、条件失效=残差监测、
独立验收=外环度量。

系统主张（本分支实现并在隔离环境中验证）：
1. 结论必须由证据承载：目标证明只认 fact/finding 节点（假设不是证据）；
2. 假设演化必须留痕可审计：整体替换式声明 + 事件流；
3. 实验期望必须预注册：派发后规格不可变，判定只认精确匹配（歧义不判定）；
4. 证据撤回必须反证传播：误报/删除 → 依赖假设同事务失效并唤醒规划器；
5. 一切关键写路径受不变量保护：CAS、行身份、追加式触发器、守门完成。

## 2. 实现地图（验收证据）

| 主张 | 机制 | 验证 |
|------|------|------|
| 证据承载结论 | prove_goal 事务边 + proves 边 | 矩阵 TestGraphOps*；M0.2 基线 |
| 假设留痕 | hypotheses/hypothesis_events + 整体替换语义 | TestHypothesisLifecycle |
| 预注册 | experiments 规格触发器 + 精确匹配判定 | TestExperimentPreRegistration |
| 反证传播 | InvalidateHypothesesOnNodeTx + false_positive 同事务 + wakeup | TestConditionInvalidationLoop；批审 P1 修复后 basis 并入闭环 |
| 不变量 | 计划 CAS/RowID、追加式触发器、CompleteTaskGuarded | TestTaskPlanPG*；34→35 矩阵 case |
| 外环 | 干净上下文 Reviewer（advisory，无写权） | M3.4 live 运行抓出 4 个真实问题（evidence/M3-3-4-local-results.json） |
| 慢环 | ChangeProposal 挖掘（需人工审批，无 AutoApply） | TestMineChangeProposals |

全部机制在真实隔离 PostgreSQL（artex_research_test，PG 17.11）35 case 矩阵
（race）+ 离线 race 套件 + CI（3 job，含真实 research-postgres）持续验证；
六轮独立只读审查（全部 APPROVE_WITH_P1）驱动修复，全部 P1/P2 已修复回归。

## 3. 行为实验结论（GLM-5.3-Flash，全部预注册计量）

### 3.1 基线（M0.2，evidence/M0-2-baseline-runs.json）
正场景有据完成、负场景拒判（不编造证明）——模型具备基线诚实性。

### 3.2 单轮消融零效应（M4.2，evidence/M4-2-pilot-results.json）
可判定单轮场景中 A/B/C/D 四变体行为无差异（pos 4/4 有据完成、neg 4/4 拒判），
认知工具零调用。**教训：消融场景必须让被测机制成为完成的必要路径**；按预注册
不追加重复（防凑方向）。

### 3.3 采用缺口（M4.3，evidence/M4-3-pilot-results.json）
判别性 decoy hunt（诱饵事实在机制上足以"证明"目标）：四变体全部有据完成、
零假证明，但认知工具几乎不被自发使用（仅一次假设声明；实验始终为零）。
并行全探在确定性契约下是最优策略——认知层的可测价值不在效率（信息论上也不
可能在），而在诚实性与可审计性。

### 3.4 纪律驱动采用（M4.4，evidence/M4-4-pilot-results.json）
显式流程纪律指令（变体 E）→ 全链路采用（声明/预注册/派发/环境执行/判定）。
对照组 D 三次因网关停滞失败（透明存档）；无纪律成功运行实验调用为零。**采用
是提示层可控变量。**

### 3.5 认知闭环端到端成立（M4.5，evidence/M4-5-results.json）★核心结果
期望书写粒度修复（简报层：一实验一意图 + 期望=观察串逐字原文）后，E 变体连续
两次完整走通：假设声明×4 → 预注册实验×4（期望逐字）→ 派发×4 → 脚本化环境执行
→ **精确匹配判定 observed=4、supported=4/refuted=4**（信念修订事件留痕）→
以环境产出证据有据证明，零假证明。假设-实验-判定-修订链在真实模型驱动下
端到端可复现运行且全程可审计。

### 3.6 系统诚实性的设计证据
- 精确匹配拒绝强拟合（M4.4 inconclusive 案例：期望措辞≠观察 → 不判定）；
- 判定口径按对抗最强解释加固（环境产出证据：summary 全等 + yields 边，批审 P2）；
- 假设不是证据的边界从未被突破（全部 run 的证明证据均为 fact 节点）。

## 4. 诚实的局限

1. 行为实验为小样本（r=1–2）、确定性脚本环境、单一模型——结论是机制可行性与
   方向性证据，不是统计效力声明。
2. 多智能体真实渗透环境下的闭环行为（噪声观察、证据中途撤回的 live 反证传播）
   未在本批 exercised——机制有存储层与矩阵测试支撑（TestConditionInvalidationLoop），
   但缺 live 场景。
3. 效率主张明确不做：确定性环境下认知层不减少探测次数；其价值主张是纪律、
   可审计性与假证明抵抗。
4. 网关不稳定导致 D 变体三次失败（存档于 M4-4 证据）；对照取跨批次成功运行
   并如实标注。

## 5. 复现与审计路径

- 全部矩阵：`ARTEX_TEST_PG_DSN=postgres://$USER@localhost:5432/artex_research_test scripts/test-research-pg.sh`（35 case，race，skip=fail）
- 离线：`go build ./... && go vet ./... && go test -race ./harness ./agent ./cmd/harness-eval`
- 行为实验：`cmd/research-ablation -mode single|hunt`（预注册与硬顶见
  task-specs/M4.2.md…M4.5.md；预算账目见 SESSION_HANDOFF.md）
- 每个里程碑的 PASS/FAIL/REVIEW 记录：evidence/（21+ 文件）；六轮审查报告：
  evidence/*-review.md；决策与教训：DECISIONS.md

## 6. 结论

分支交付了它承诺的东西：一个证据驱动、假设显式化、实验预注册、失效反证传播、
外环可审计的认知智能体研究实现——其核心闭环（§3.5）已被真实模型端到端走通并
可复现，其诚实性边界（§3.6）在全部实验中保持。这构成"像人类专家一样在部分
可观测环境中推理"这一研究路线的机制层证据；统计层与真实环境行为是后续工作。
