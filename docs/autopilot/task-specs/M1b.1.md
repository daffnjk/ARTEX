# M1b.1 — Task plan persistence and real Planner integration

Predecessor: M1a.1 code be0d4e65, tested/reviewed; frozen contract unchanged. Split storage (a), SDK/Planner integration (b) and complete real-PG matrix (c); none alone claims all M1b.1 requirements.

## Source and call-path inventory

Root go.mod pins Go 1.26.3 and norma v0.4.3. agent/planner.go stores todos in a map keyed by exploration ID and passes a concrete *tool.TodoStore to agentcore.Options.Todos. server/task_llm.go creates replacement Planner instances when task-agent bundles are rebuilt. This loses the process-local map on replacement/restart. SDK Todo contains only content/status and has no exported setter or persistence interface. db.Open applies embedded idempotent schema under a dedicated connection's advisory lock. Existing db tests may skip on missing/unreachable development DSN; the new dedicated test suite must not silently skip.

Task rows use stable numeric task IDs and reference explorations. Archive snapshots/compaction/restoration explicitly enumerate tables, and tasks may remain as deleted/archived stubs. New plan rows must follow actual deletion/archive/restore behavior, not just add a tasks foreign key. No second global planner or execution tool is introduced.

## Acceptance and proposed boundaries

Task-owned PostgreSQL plans have monotonically increasing revisions/CAS, stable step IDs independent of display order/text, basis references, output placeholders/bindings and a separate model-notes field. Persist each successful write before acknowledging it. Stale/concurrent writers fail with a recoverable explicit conflict; do not silently overwrite or blindly replay. Reopening/recreating a Planner must load the stored plan. Task identity comes from the host, not model arguments. Todo status/completed is only a planning note, not proof or goal completion.

Implement against the real SDK's actual tool-registration path. A database read/write error or failed hydration must stop the planning run rather than fall back to empty memory. Run normal, malformed/bounded, ownership, stale revision, simultaneous write, cancellation, close/reopen/new-instance, delete, archive/restore and migration-twice cases on a dedicated resettable PostgreSQL database. Reject missing test DSN or unexpected non-isolated address/database identity in the explicit integration runner. Preserve failures and test logs. No production migration.

No automatic downstream execution is added for plans/output bindings; graph transaction and final completion integration retain their own dependencies. Old archive formats without plan data remain explicit empty plans, not reconstructed assertions. Document migration and rollback; rollback must not silently discard stored plans. Current Go project tests, relevant server compilation, real SDK registration tests and CI path selection are required. A type definition, in-memory mock or skipped PG suite does not pass this unit.

## Design decisions (2026-10-18, research/autopilot-v2, local-only session)

Cognitive role: the planner todo list is the planner's control law sketch — the serial chain of intended actions with its basis. Persistence gives the controller state reachability across Planner replacement (task-agent bundle invalidation) and process restart; CAS prevents a stale controller (old Planner instance) from silently overwriting a newer one. Plan status remains a model note: it is never an acceptance or completion token (no completion path reads `task_plans`).

Binding point (real SDK path, norma v0.4.3 verified in module cache):
- SDK `TodoStore` exports only `List()`; `set()` is private; `agentcore` appends its own `TodoWrite` tool whenever `Options.Todos != nil` (registry: later same-name registration wins, so the SDK tool cannot be wrapped via Options). `harness.maybeInjectTodoReminder` is nil-guarded, so `Options.Todos = nil` is safe (in-run reminder off; the planner already re-renders the list at every wake-up).
- Persistent mode therefore registers a project-owned `TodoWrite` CoreTool in the planner base tool list (through AugmentTools → ToolResolve; `tools` table has no TodoWrite row, so it passes through untouched), with the SDK-compatible schema plus optional structured fields per item (`basis_node_ids`, `expect`) and one plan-level `note`. Every successful call persists via CAS before the tool acks. Legacy in-memory mode (plan store nil) keeps the exact current behavior for offline use; production wiring always sets the store.
- Step identity: host-assigned stable IDs; a step keeps its ID when its exact content reappears (order-independent); new content gets a new monotonic ID; edited text is a new step (SDK whole-list-replace interface cannot express "same step, edited text" — recorded limitation). Basis ids are validated to belong to the same exploration.
- Storage: `task_plans` keyed by `exploration_id` UNIQUE (tasks↔explorations are 1:1), `revision` CAS counter starting at 1, `steps` JSONB, `model_notes` TEXT. Save = `INSERT … ON CONFLICT … DO UPDATE … WHERE revision = expected` — 0 rows ⇒ typed conflict error carrying the current revision; never last-write-wins; first-save race resolves one winner.
- Archive/delete: the plan is a regenerable working artifact, not evidence — it is deliberately NOT part of the archive payload (format_version unchanged; restored tasks hydrate an explicit empty plan). The archive purge enumeration adds `task_plans` next to `activity`/`exploration_nodes`; hard exploration delete cascades via FK.
- Revision history (plan evolution audit) is deferred to M3 where belief revision needs it; recorded in DECISIONS.md.
- Rollback: `DROP TABLE IF EXISTS task_plans` is explicit and logged; it discards plans only (model scratch, regenerable); a pg_dump preserve command is documented in the runbook.
