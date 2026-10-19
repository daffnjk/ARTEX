# M1b.2a — Graph write-path inventory and atomic domain operations

Predecessor: M1b.1c (merged into research/autopilot-v2 locally). Read-only
inventory completed 2026-10-18 against commit 3ce6272 (Explore agent report
preserved in this file's appendix and in the session record).

## Inventory conclusions

- db-layer atomic units are solid: AddNode/AddIntent/AddGoal, Anchor/Link
  (idempotent ON CONFLICT), intent CAS (ClaimIntent/CompareAndSetIntentState),
  SoftDeleteIntent/CancelIntent/DiscardOpenIntent, digest ops, RecordFindingTx
  (WithEvidenceTx advisory lock + task/intent boundary checks), task
  create/delete/archive/restore transactions.
- No network/exec inside DB transactions anywhere (full scan). Local fs I/O
  inside evidence transactions is a deliberate anti-race design (traffic/evidence.go
  comments) — recorded, not changed in M1b.2a.
- HIGH gaps, all one disease — node insert and lineage edge insert are separate
  autocommit statements with swallowed `_ = Link(...)` errors:
  C1 prove_goal (agent/tools.go:1183-1184: proves edge + 'met' flip separate,
  both errors swallowed, tool still returns success text; allMet computed
  in-memory from a racy re-read — C6);
  C2 add_intent (tools.go:1071-1087 intent node vs derived_from/origin edges);
  C3 record_fact (tools.go:1342-1347 fact node vs yields edge);
  C4 goal+spawns edge split at three entrances (tools.go:1462-1467 set_goals,
  server/goals_api.go:63-69 UI addGoal, server/goals.go:398-406 decomposer
  fallback); C8 same pattern in server.go:1722 seedFirstIntent.
- MEDIUM: C5 setFindingCol two-statement non-tx mirror (db/findings.go:718-733);
  C7 worker final-state write failure only logged (engine.go:1149-1151).

## Implementation (this unit)

1. db/exploration.go: one transactional primitive family reusing the AddNode tx
   pattern — node + its lineage edges commit atomically (edges ON CONFLICT
   DO NOTHING for idempotency; any edge failure rolls back the node):
   - AddIntentWithParents(payload, priority, state, origin, anchors, parentIDs, fallbackOrigin)
   - AddFactWithYields(payload, priority, state, origin, anchors, intentID) (yields edge)
   - AddGoalWithRoot(payload, origin) (spawns edge from origin)
   - ProveGoal(goalID, evidenceNodeID): tx + FOR UPDATE + kind check + proves
     edge + predicate CAS flip open→met; concurrent/second proof is an explicit,
     recoverable outcome, never a silent overwrite.
2. agent/tools.go: replace the four swallowed-error sites with the new
   primitives; failures return tool errors (the model must see that nothing was
   recorded). prove_goal completion check switches to the authoritative
   HasOpenGoal SQL (kills the C6 in-memory race) and only reports met when the
   flip actually committed.
3. server/goals_api.go, server/goals.go, server/server.go seedFirstIntent:
   swap to the same primitives (C4/C8).
4. db/findings.go: setFindingCol inside WithEvidenceTx (C5) so the findings row
   and the node-payload mirror commit or roll back together.
5. C7: bounded retry (3 attempts) around the worker final-state CAS, failure
   still logged and left to startup ResetRunningIntents (documented, unchanged
   semantics).
6. NOT changed (recorded decisions): fs I/O inside evidence tx (anti-race by
   design); goal_met as in-process signal only (no DB object); ControlWork
   timeout window (bounded, low).

## Acceptance

- Regression tests on real isolated PostgreSQL: edge-FK failure rolls back the
  node insert (no orphan nodes without lineage); prove_goal is atomic and
  idempotent-safe (repeat proof / proof of non-goal / proof of met goal are
  explicit outcomes, no double flip); fact+yield and goal+spawn and intent+
  parents commit together; setFindingCol mirror rollback on failure.
- Tool layer returns failures instead of success text when recording fails
  (unit-testable against the db layer with a real PG or injected failure).
- Existing suites (harness/agent/db/PG matrix) stay green; no behavior change
  for callers that only consumed the atomic units already present.
