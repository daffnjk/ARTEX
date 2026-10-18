# M1b.3b — Shared transactional authorized completion operation

Predecessors: M1b.2b, M2.2, M1b.3a (all DONE). Inventory input: task-specs/M1b.3a.md.

Cognitive role: completion is the outermost control action of the loop. It now
happens in exactly one place, inside one transaction, revalidating the world
as-of the write: no open goals remain (authoritative re-read under row lock —
the HasOpenGoal check and the write can no longer race), every goal referenced
by a CURRENT frozen acceptance version is met (the acceptance.go "final
completion must atomically revalidate" semantics), the terminal write is CAS
(first terminal state wins — fixes inventory R6: a late planner-met can no
longer overwrite an earlier timeout), and the completion SOURCE is recorded
separately (model:prove_goal / system:goalless-drain / system:timeout).

## Delivered

- schema: tasks.completed_source (idempotent).
- db.CompleteTaskGuarded(taskID, status, source): single tx — task row lock,
  done-specific graph consistency + current-acceptance revalidation (typed
  ErrCompletionGoalOpen / ErrCompletionAcceptanceUnmet, recoverable), terminal
  CAS, source recording. Non-terminal statuses rejected.
- Manager.CompleteTaskGuarded wrapper (lifecycle mirror); the three inventory
  completion sites now route through it: planner-met (was non-CAS SetStatus —
  R6 fixed), goalless drain, timeout settlement.
- No new auto-release: this operation validates graph goal state and frozen
  acceptance versions; it does NOT consume EvaluationReport (deferred to M2.3
  controlled integration per the frozen contract). Review-mode tool gating
  unchanged. R3 (goalless drain as the ungated done path) remains a DOCUMENTED
  risk — now at least recorded with source and subject to acceptance
  revalidation when acceptance rows exist.

## Acceptance (verified, isolated PG 17.11)

TestCompleteTaskGuardedSemantics: open goal refuses (typed error, no terminal
write); all-met completes with source; late terminal does not overwrite (state
AND source preserved); current acceptance referencing an abandoned (settled,
unmet) goal refuses completion — the acceptance check is reachable
independently of the open-goal check. Matrix 25/25 PASS with race; build/vet/
agent/harness regressions green; the 5 pre-existing environment-dependent
server tests fail identically before and after this change (verified by
stash-baseline comparison; CI paths unaffected).
