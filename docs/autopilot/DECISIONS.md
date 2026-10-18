# Development decisions

## 2026-10-17 / GOV.0

- Live main is 674dd067eb4eba7dc95b786a63b7576abb064a48. Existing research/evidence-harness-m1 is behind by one merge commit and ahead by zero, with an identical tree. Created research/autopilot-v1 from the merged baseline; no unmerged work discarded, old branch unchanged.
- Root AGENTS.md returned 404; recursive tree contained no AGENTS.md or autopilot files. Do not overwrite unrelated instructions.
- Direct local git clone failed at DNS. Prefer the connected GitHub API and an artifact of committed source from the existing, read-only, time-bounded research workflow. No new service, credential scope or paid fallback. A source bundle contains Git objects only, not checkout credentials or untracked environment files. It is an acquisition/provenance aid, not a test result.
- Freeze the owner's finite scope and authority in PROJECT_CONTRACT.md. Dependency splitting does not remove parent requirements. State records are scheduling hints, never completion evidence or runtime authorization.
- State cannot contain its own eventual commit hash. checkpoint_parent_commit identifies the observed predecessor; git log identifies the state-file commit; subsequent checkpoints link verified code commits and immutable evidence. Never treat an untested metadata update as code validation.
- M1a.1 is first: current evaluation.go chooses the verifier by request criterion/version. No current dynamic reproduction or fix is claimed yet. Missing trusted config must fail closed. Database integration remains M2.

## 2026-10-17 / M1a.1

- Exact request-to-frozen-spec matching is deliberate. Model semantic matching would widen authority; a changed experiment/context needs a newly trusted frozen binding rather than requester choice.
- Retain the old constructor for empty-registry integrity/CLI compatibility, but an unbound nonempty registry is now fail-closed. Host callers use NewEvaluatorWithAcceptance; the CLI does not ingest goal specs.
- Reread after verification and discard changed state. This is not a database transaction or defense against changes after the return; final-write CAS remains a mandatory M1b.3/M2 dependency.
- Retained the pre-fix dynamic failures and all existing assertions. Real current-code project CI and independent review are separate from successful local stdlib tests.
- .gitignore ignores new docs globally; explicitly stage only these reviewed autopilot records with git add -f, without weakening repository ignore rules.

## 2026-10-17 / M1a.1 acceptance and M1b.1 preparation

- be0d4e65 passed current scoped Go 1.26.3 CI and separate Codex read-only code review (receipt 6236232397). The agent CI tested temporary PR merge 60dedbff, whose tree was checked equal to the branch commit; main was not merged or modified.
- Import API-created Git objects only after reproducing the full exact SHA. The API normalizes dates to UTC but this commit retains +0800 raw headers; no history or author information was invented or rewritten.
- norma v0.4.3 tool/todo.go (blob e574768836ed068becd3e699fe1140cf59ae615a) exposes concrete TodoStore/List/Tool, private set, and content/status only. Do not invent a pluggable persistent-store API; inspect agentcore registration before integration. Todo status remains a model note, never an acceptance record.
- Acquire only the project's real public Go toolchain and vendored dependencies through the existing read-only 15-minute CI job, once. No unrelated cache, credentials or environment dump; compressed artifact capped at 160 MiB, retention one day. The request marker must be removed after successful acquisition. This neither adds a paid service nor changes budget limits; cost/quota remain unknown. Acquisition failure remains explicit.

## 2026-10-17 / Real SDK acquired, PostgreSQL validation preparation

- Verified artifact 12106468826 and its inner SHA; root go.mod/go.sum are byte-identical. Actual local Go 1.26.3 core/race and completion guard tests pass. Vendored public source stays outside the repository with a local-only ignored symlink. Removed the SDK request marker; no continuing export is implied.
- Obtain only the never-started public postgres:16-alpine image already used in repository compose. The existing read-only CI job remains capped at 15 minutes; image 512 MiB, compressed archive 160 MiB, retention one day. No running database or volume export, no environment/credential dump. Current executor already has chroot; no permission increase is allowed. Failure leaves local PG unverified and the dedicated CI test-service path available.
- Local M1b.1a storage/archival code and tests are in progress, not yet committed or accepted. Unit/race and archive-package tests passed, but no real PG result is claimed. Missing-path DSN validator regression was reproduced and fixed (one strategy); the explicit suite fails rather than skips on missing or non-isolated configuration.

## 2026-10-18 / M1b.1 on research/autopilot-v2 (new local session, real PG available)

- Baseline re-verified before any work: remote main moved to cc8acc9d (PR #2 merged); docs/autopilot state exists in-repo; the previous session's uncommitted M1b.1a edits lived in /mnt/data/artex-work/ARTEX (a container workdir) and are NOT present here — this session reimplemented M1b.1 from the current main baseline rather than assuming that work survived. Baseline tests were re-run locally (build/vet/race harness/completion gate all PASS) before implementing.
- Branch: research/autopilot-v1 was merged and deleted upstream; recreated name would conflate PR history, so this session works on research/autopilot-v2 from cc8acc9d (recorded, not silently reused).
- Real environment this session: local PostgreSQL 17.11 with a dedicated artex_research_test database; the previous ENV-PG blocker is resolved here without the CI image-export workaround. The runner (scripts/test-research-pg.sh) and the suite itself both reject missing DSN and non-local/non-artex_research_test targets; no production DSN used.
- Divergence from the lost draft: plans are a single authoritative row per exploration with a revision CAS counter — no plan-revision history table and not part of the archive payload. Rationale: plans are regenerable model scratch, archives keep evidence; history is deferred until M3 belief revision actually needs it (recorded, reversible).
- SDK binding point (verified against norma v0.4.3 in module cache): agentcore appends its own TodoWrite whenever Options.Todos != nil and Registry.Add lets the later same-name tool win, so the SDK tool cannot be wrapped via Options. Persistent mode therefore sets Options.Todos = nil (harness reminder is nil-guarded) and registers a project TodoWrite CoreTool through AugmentTools → ToolResolve; the tools table has no TodoWrite row so it passes through untouched. Legacy in-memory mode retained for offline/test by leaving SetPlanStore unset; production wiring always sets it.
- Step identity is exact-content matching (order-independent). Editing a step's text yields a new step id — the SDK whole-list-replace interface cannot express "same step, edited text"; recorded as a known limitation rather than papered over with fuzzy matching (fuzzy matching would mis-bind basis references).
- Hydration failure stops the planning round (fail-closed); a store write failure acks nothing and surfaces "plan NOT saved". Plan status remains a model note: no acceptance/completion path reads task_plans.
- CI research-postgres job (postgres:16-alpine service + the runner script) added; YAML validated locally, job NOT_RUN until the branch is pushed (user chose local-only commits this session; push requires gh auth). Recorded as NOT_RUN, not as green.
- Repo-local git identity set to the account's existing author identity (fsdgGFF <53821161+daffnjk@users.noreply.github.com>, matching merged PR #2 history); no global config changed. docs/ is gitignored; autopilot records are staged with git add -f per existing precedent.

## 2026-10-18 / M1b.1 independent review and CAS hardening

- Read-only reviewer (separate subagent) returned APPROVE_WITH_P1 with two real CAS holes: (1) the INSERT branch ignored the expected revision, so a stale in-flight writer could resurrect a plan deleted by archive purge — and that dirty row would then survive restore; (2) delete→recreate resets revision to 1, letting an old writer holding (1, oldRowID) silently overwrite the recreated row (ABA).
- Fix: TaskPlan carries the storage RowID; SaveTaskPlan is now three explicit steps — CAS UPDATE matching (revision AND row id), INSERT only for the (0,0) first-write expectation under ON CONFLICT DO NOTHING, then a typed conflict probe where an absent row yields PlanConflictError{HaveRevision:0} ("plan deleted/archived — expectation stale", surfaced distinctly to the model). A first single-statement SQL attempt (guard inside INSERT…SELECT…WHERE) was itself caught by the matrix — normal updates degraded to conflicts because ON CONFLICT DO UPDATE never fires when the SELECT yields no rows — and was replaced; the failure is recorded, not hidden.
- Reviewer P2s also fixed: DSN guards (script and in-test) now reject any query parameter (pgx applies ?host=/?database= after the URL authority; negative-tested), and the dedicated runner fails when the archive regression SKIPs instead of printing PASS. Matrix extended: row-deleted stale write, delete-recreate ABA, true concurrent same-revision double write — 11 cases + archive regression, all PASS with race.
- The review pass also confirmed: the diff incidentally repaired a digit-transposed upload-artifact pin in an existing job (…4886f43607fa02 → correct v4.4.4 …4886f3607fa02) that would otherwise have failed CI action resolution; kept as a fix.

## 2026-10-18 / CI pin lessons (push + PR #3)

- The M1b.2a reviewer's claim that the workflow's upload-artifact pin was a digit-transposed typo was wrong; adopting it broke two jobs. Reverted — same-run evidence (one job resolving the original pin while two failed on the "corrected" one) settled it in minutes. Lesson recorded: reviewer assertions about external facts need empirical verification before adoption, exactly like model output.
- The real second failure was my own transposed checkout pin in the new job (...67726e vs ...677262) — caught by CI, confirmed via API. All pins in research-harness.yml are now identical across jobs and verified resolvable.
- Push/PR used the macOS keychain git credential via GH_TOKEN injection (gh CLI login remains unconfigured; ~/.config/gh is sandbox-writable-blocked). Token never printed or persisted to disk by the executor.
- CI run 39130370327 (commit ecadba4): all three jobs green — research-postgres ran for real on a postgres:16-alpine service container, closing the CI-NOT_RUN entries in evidence.

## 2026-10-18 23:55 M4.2 记录（诚实零效应）
- Pilot r=1：四变体 pos 全有据完成、neg 全拒判，零假确认；认知工具在 C/D 已注册仍零调用
  → 场景对认知层无区分力。按预注册"不追加运行凑方向"条款，r=3 不执行。
- 教训（观察 vs 解释）：消融场景设计必须让被测机制成为完成的【必要路径】，
  否则得到的是无区分力的零效应（本 pilot 即实例）。
- M3.2–M4.1 独立审查（agent_068cba5d）APPROVE_WITH_P1：P1=supported 假设不合并
  区分性证据 fact 到 basis_node_ids，失效监测漏网；P2×7 见 evidence/M3-2-M4-1-review.md。

## 2026-10-19 00:15 批审 P1 修复 + 级联删除认知史
- supported 假设的证据链闭合：区分性 fact 并入 basis（否则失效监测按 basis
  检索会漏掉真正的支撑证据撤回）。
- DeleteTaskCascade 与追加式触发器的冲突是结构性 bug（任务删不掉 + 测试清理
  静默失败累积残留）。选择 owner 级事务内 DISABLE/ENABLE 触发器而非
  session_replication_role：后者要 superuser，owner 权限即可且 DDL 事务性
  保证回滚还原。
- 测试清理路径必须与被测代码同源（m31Store 改走 DeleteTaskCascade 后，
  研究库残留从 192/123 清零并保持为零——复跑矩阵即验证）。

## 2026-10-19 00:35 M4.3 发现：采用缺口
- 工具注册+信念渲染不足以让模型自发采用假设机制（live：仅 D 声明过假设，
  实验预注册/派发全零）。M4.4 用显式纪律指令检验可驱动性。
- 定确定性契约场景里并行全探最优——认知层的可测价值应定位在诚实性/可审计性
  （假证明抵抗），效率增益在信息论上不可期待。场景设计据此转向。

## 2026-10-19 01:00 M4.4 发现：纪律指令驱动采用，闭环机制 live 走通
- 无纪律：实验工具零采用（多 run 一致）；显式流程纪律：全链路采用（声明/预注册/
  派发/环境执行/存储判定）。采用是提示层可控变量。
- 精确匹配判定的真实摩擦：模型期望措辞≠观察串原文 → inconclusive（拒绝强拟合，
  系统按设计工作）。期望书写规范是下一打磨点。
- 网关传输错误（unexpected EOF）两次打断 D——基础设施失败透明存档，不隐藏。

## 2026-10-19 01:00 M4.4 发现：纪律指令驱动采用，闭环机制 live 走通
- 无纪律：实验工具零采用（多 run 一致）；显式流程纪律：全链路采用（声明/预注册/
  派发/环境执行/存储判定）。采用是提示层可控变量。
- 精确匹配判定的真实摩擦：模型期望措辞≠观察串原文 → inconclusive（拒绝强拟合，
  系统按设计工作）。期望书写规范是下一打磨点。
- 网关传输错误（unexpected EOF）两次打断 D——基础设施失败透明存档，不隐藏。

## 2026-10-19 01:45 深夜批审修复要点
- 模型上报的标识（factID）并入任何不变量结构前必须过与声明路径相同的校验——
  「修复引入的旁路」是 P1 的常见来源（本次 basis 并入即实例）。
- DISABLE TRIGGER 类操作必须先以 LOCK TABLE ACCESS EXCLUSIVE 定锁序，否则与
  并发写事务成环死锁。
- 研究运行器的判定口径要按【对抗最强解释】写（Contains→全等+yields 边）；
  清理路径的错误必须上报而非丢弃。

## 2026-10-19 02:10 D 变体三次网关失败处置
- 同根因（网关停滞/EOF）三次尝试后停止重试（约束上限）；对照口径取 M4.3 成功
  D 运行并如实标注跨批次。第三次失败前的自发 state_hypotheses×4 是有效观察。
