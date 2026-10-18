# M0.2 + M1b.2b batch independent review — report and disposition

Reviewer: separate read-only subagent (agent_062a7276), 2026-10-18.
Scope: `git diff c5ede8b..ed23873`. **Verdict: APPROVE_WITH_P1.**

## P1 findings and disposition (both fixed)

- **P1 runner cleanup bypass**: os.Exit skipped deferred cleanup on every
  failure path, leaking seeded rows into the research DB exactly when runs
  failed (violating the runner's own contract). → FIXED: restructured to a
  single-exit run() error flow; defers now always execute.
- **P1 tool_calls double-count + evidence misreading**: the collector matched
  both tool_use and tool_result activities (both carry the tool name), so each
  call appeared twice; the original evidence notes (and a BACKLOG note)
  misread this as repeat calls — including the claim "prove_goal called twice /
  idempotent repeat observed live", which was WRONG (each tool was called
  once; readback proves_edges=1 confirms). → FIXED: collector counts tool_use
  only; evidence and BACKLOG notes corrected with the misreading explicitly
  retracted; raw run data retained. A clean lesson in observation-vs-
  interpretation discipline: raw data was true, the interpretation was not.

## P2 findings and disposition

- URL-form DSN enforcement in the runner (keyword/value form could bypass the
  host guard; profile path was read-only so impact bounded) → FIXED.
- Per-request output cap (32k) so a single response cannot overshoot the total
  budget silently → FIXED.
- prove_goal tool return text still claimed "all goals met" while abandoned
  goals also settle completion → FIXED (aligned with the reason semantics).
- graph_integrity LIMIT-suffix trimming was fragile → now asserts the suffix
  and fails loudly.
- dangling_tool_use flagged NULL-id legacy rows forever → filtered out with a
  note (separate classification deferred).
- finding_node_broken identifiability (no findings.id surfaced) → recorded,
  cosmetic, deferred.
- hard-mode repeat-cancel asymmetry + a routes-level test for the idempotent
  cancel → recorded as deferred with the server-harness batch.

## Clean areas (reviewer confirmed)

Metering correctness (failed requests count against the cap; check-then-
increment under lock; Stream path disabled loudly); no key leakage surface
(errors carry no Bearer; output JSON carries name/model/format only);
task_control idempotent-cancel branch placement and safety (deleted is
terminal; no misjudgment path; side-question cleanup already atomic);
fallbackChat swap complete (no bare AddIntent callers remain anywhere);
no P0 findings (no injection, no secret leak, no production-data surface).
