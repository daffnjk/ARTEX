# M2.1–M3.1 batch independent review — report and disposition

Reviewer: separate read-only subagent (agent_143a00b5), 2026-10-18/19 night.
Scope: `git diff 35bb667..d9daa93`. **Verdict: APPROVE_WITH_P1.**

## P1 findings and disposition (both fixed)

- **P1 expectation matching was bidirectional-Contains** — expected="200"
  matched observed="3200"; short-string coincidences could produce false
  unique matches, wrongly supporting one hypothesis and IRREVERSIBLY refuting
  the rest. → FIXED: exact (trim-equality) matching only; matched texts are
  recorded in the outcome JSON for audit; tool guidance now states
  expectations are exact one-line observations.
- **P1 declaration consistency unvalidated** — expectations keys vs
  hypothesis_ids could diverge (non-participants "supported", participants
  batch-refuted, cross-exploration corrosion) and dispatch didn't check
  intent ownership. → FIXED: DeclareExperiment enforces keys ==
  participants, all same-exploration ACTIVE hypotheses; DispatchExperiment
  validates the intent belongs to this exploration.

## P2 hardening applied in the same commit

- Structural anti-collapse: a question under investigation cannot be restated
  to fewer than 2 competing hypotheses (collapse is driven only by experiment
  outcomes).
- hypothesis_events: no more synthetic events for rows that didn't actually
  transition (RowsAffected checked) — protects the M3.2 wakeup feed.
- Basis nodes must be fact/finding (not just any same-exploration node).
- schema: partial UNIQUE (task_id, goal_id) WHERE is_current (single-current
  invariant structurally enforced); TRUNCATE guards on the three immutable
  tables; harness_evaluations.request_id UNIQUE with idempotent re-record.
- Manager revive clears completed_source (no stale provenance on revived
  tasks); planner-met skips worker cancellation when completion legitimately
  failed (open goals / acceptance unmet / already terminal); inspection
  distinguishes 404 from DB 500; belief-state render failures surfaced to the
  model instead of silently dropped, render capped at 16 KiB.
- Test hygiene: run-unique request ids where the new UNIQUE index exposed
  cross-run residue (one-time dedupe of the disposable research DB performed
  via session_replication_role, recorded here).

## Recorded, deferred

- Superseded statements are not silently revived on restatement (model sees
  only active in the render) — decision recorded; revive flow belongs to M3.2.
- fact_id in report_experiment_outcome is recorded but not FK-verified
  (verification lands with M3.2 basis tracking). Scoped/global integrity SQL
  remain two copies with a suffix guard on one side. Tool-level unit tests
  beyond validation paths deferred with the server-harness batch.
- Reviewer confirmed clean: immutable triggers coverage, acceptance idempotency
  completeness, CompleteTaskGuarded CAS + acceptance revalidation, R6 fix
  legitimacy, inspection auth, tool registration chain (seed/ToolResolve/
  nil-store guards), single-trigger-per-table ordering, JSONB map round-trips,
  and NO P0 findings.

## Re-verification

Research matrix 27/27 PASS with race on isolated PostgreSQL 17.11 (log sha256
d7b3378b439984749e23b1e1dc18ba8f7d9a6f2e478613dbad0a27d7a1cf0190); build,
vet, agent, harness/cmd, and the inspection assembly all green.
