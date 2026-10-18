# M1b.3a — Completion-path and authority inventory (no behavior change)

Read-only inventory completed 2026-10-18 against ed23873 (Explore agent,
file:line evidence verified below). This unit changes NO behavior; it pins the
map every later completion gate (M1b.3b/M2.3) must reason about.

## A. Completion entry points (all of them)

| # | Entry | Location | Trigger | Writes | Gated? | Evidence check |
|---|---|---|---|---|---|---|
| 1 | prove_goal tool | agent/tools.go:1146 → atomic ProveGoal db/exploration_atomic.go:138 | model (planner default) | goal open→met; last one ⇒ GoalMet flag | yes: guardGoalCompletion wraps AFTER DB tool-resolution (agent/assembly.go:63) | structural only (goal/evidence kind + ownership, tools.go:1164-1171); quantitative acceptance is prompt discipline only (planner.go:328) |
| 2 | goal_met tool | agent/tools.go:1189 | model — default UNBOUND (toolcatalog.go:59, orchestration.go:549); admin can rebind in UI | nothing in DB; blind process flag | yes (same name gate) | none |
| 3 | goalless drain → done | server/engine.go:843-857 (no open goal AND no active intent) | system (human-triggerable via deleteGoal) | SetTaskStatusGuarded "done" | **NO** — bypasses prove_goal entirely | none |
| 4 | planner met → done | server/engine.go:880-894 | system executing model intent (met only from #1/#2) | SetStatus "done" (non-CAS!) + AbortGoalMet | indirect | inherits #1/#2 |
| 5 | timeout settlement | server/engine_timeout.go:183-227 → final planner round (wrapup.go:122) | system | done-if-met else timeout, Guarded | met branch inherits tools; review ⇒ timeout | none independent |
| 6 | intent terminal states | server/engine.go:1131-1160 | system | intent node states only | n/a | n/a |
| 7 | task control API | POST /api/tasks/{id}/control — pause/resume only (server.go:1114); NO human "complete task" endpoint exists | human | no done writes | — | — |
| 8 | goals API | server/goals_api.go add/edit/delete | human | no direct done; deleteGoal can empty goals ⇒ #3 | no | none |
| 9 | model-side revive | set_goals / add_intent ⇒ resumeTask ⇒ reviveTask (goals.go:349) | model | done→running (authority surface, not completion) | no | none |
| 10 | rerun/control intents | server.go:1194+ | human | terminal→running | no | none |
| 11 | archive | task_archives.go:220+ (requires paused/terminal) | human | deletion-style closure, not done | — | — |
| 12 | on_goal_met trigger | scheduler.go:184 fires platform agents only | system | no task status writes | — | injected copies rejected by needExploration (tools.go:322) |
| 13 | direct SQL | ONLY db/tasks.go:480 SetStatus, :493 SetTerminalStatusGuarded, server/manager.go:1166 ApplyTaskAdmission (never writes done) | — | — | — | — |

## B. State machine facts

- tasks.status writers are exactly three functions (verified by grep);
  running→done only via engine.go:888 (SetStatus, non-CAS) and engine.go:850 +
  engine_timeout.go:218 (Guarded). "failed" has NO writer; 'paused' value has
  no writer (paused is a boolean column).
- goal open→met only via ProveGoal (tx CAS). "abandoned" has NO writer — the
  prove_goal reason text documents it as settled-if-ever-present.
- completed_at written only inside the three status functions (terminal
  COALESCE-keeps first stamp; revive clears; timeout→running resets clocks,
  manager.go:1184-1190).
- explorations.status 'achieved' is a dead value (no writer) — audit noise.

## C. Completion gate today

guardGoalCompletion intercepts exactly {prove_goal, goal_met} after DB
tool-resolution (assembly.go:63-66; cannot be re-enabled by bindings; model
params can't opt out; completion_guard_test.go:24-80). EvaluationReport /
AcceptanceResolver (M1a/M2.2) have ZERO runtime wiring — the only consumer is
the offline CLI.

## D. Premature-release risk register (input to M1b.3b)

- R1 HIGH legacy prove_goal self-certification (evidence kind-checked, content
  never verified; quantitative check is prompt-only).
- R2 HIGH goal_met rebind ⇒ zero-evidence closure in one call.
- R3 HIGH goalless drain is the ONLY ungated done path and fires in review
  mode too (human-removable goals / empty goal fallback, goals.go:394-405).
- R5 MED timeout wrap-up prompt pressures "final met judgment" (amplifies R1).
- R6 MED engine.go:888 uses non-CAS SetStatus: a late planner-met can overwrite
  timeout→done (Guarded protects itself, not against later SetStatus).
- R7 MED model can flip done→running via set_goals/add_intent.
- R8/R9/R10 LOW settlement-stage prove_goal availability; misleading injected
  goal_met "success"; dead enum values.

## E. Pinning tests added in this unit (no behavior change)

1. Source-scan test asserting the tasks.status write surface stays exactly
   the three functions (db + server packages).
2. goal_met is intercepted by the same gate as prove_goal (wiring-level;
   previously only prove_goal was tested).

Deferred to M1b.3b (needs engine/server harness or behavior decisions):
goalless-drain conditions pinned by test; review-mode DB-level assertions
(state stays open / task not done after blocked prove_goal); revive-entry
enumeration test; R6 CAS unification inside the shared transactional
completion operation with atomic acceptance revalidation
(harness/acceptance.go:43-48 semantics).
