# Session checkpoint — night 2026-10-18/19: through M4.5; cognitive loop demonstrated end-to-end live

Branch research/autopilot-v2 (pushed through da78595; draft PR #3; CI green
through run 39141180136, later pushes auto-trigger ~1 min each). Every milestone
now has real-PG verification and evidence under docs/autopilot/evidence/.

## Where we are

All backlog milestones DONE except FINAL: M0.1/M0.2 (scenario registry + live
metered baseline), M1b.1–M1b.3 (plan CAS persistence, atomic graph writes,
integrity reconciliation, guarded completion), M2.1–M2.3 (immutable harness
storage, PG acceptance source, inspection API), M3.1–M3.4 (hypotheses +
pre-registered experiments, condition invalidation + wakeup, explainable
selection with deny filter, clean-context reviewer — live run caught 4 real
issues), M4.1 (offline change-proposal mining), M4.2 (fixed-budget A/B/C/D
ablation — honest null: scenario had zero discriminating power, cognition tools
never invoked; recorded per preregistration's no-fishing clause, r=3 not burned).

Five independent review cycles total, all APPROVE_WITH_P1 → all P1+P2 fixed and
re-verified. Latest batch (M3.2–M4.1, agent_068cba5d) fixed at da78595: P1
supported-hypothesis basis union; dispatch-time deny enforcement; cascade delete
now handles cognition history (owner-level in-tx trigger bypass — DDL
transactional); fail-closed packet/notification/mining reads; event gating.

Runner: scripts/test-research-pg.sh — 34 cases, all PASS with race on
artex_research_test (PG 17.11); go vet ./... clean; research DB residue purged
and proven clean by matrix re-run. Known: 5 server tests fail on env-config
absence identically before/after (stash-verified; CI paths unaffected).

## What remains (FINAL checklist)

1. Invariant inventory cross-check: every contract invariant ↔ the check that
   enforces it (matrix case or trigger) — RUNBOOK.md §3 is the map, verify each
   line still true after tonight's fixes.
2. CI green on da78595 (auto-triggered).
3. HANDOFF/STATE/BACKLOG final refresh; M1b.3a is DONE in effect (inventory +
   pinned write surface) — flip its BACKLOG status with evidence pointer.
4. 09:00 stop: finish current minimal unit, CronList → delete automation
   "ARTEX 自主推进循环…" (id automation-bbecd691-7c7e-4cc6-97a4-9cc553002dbb),
   output session summary.

## Late-night additions (M4.3–M4.5 + 2nd review cycle)

- M4.3 decoy-hunt discriminative ablation: zero false proofs everywhere; core
  finding = ADOPTION GAP (cognition tools almost never spontaneously used).
- M4.4 discipline-directive variant E: full-chain adoption driven by explicit
  process discipline; mechanism loop ran live; experiment judged inconclusive
  (exact-match refused a non-verbatim expectation — honest no-forced-fit).
- M4.5 expectation-granularity fix (in the scenario brief, not planner code):
  BOTH runs reached observed=4, supported=4/refuted=4 — COMPLETE cognitive
  loop (hypotheses → pre-registered experiments → environment execution →
  exact-match discrimination → belief revision → grounded proof) demonstrated
  end-to-end with a live model, reproducibly. Milestone result for the
  evidence-driven cognitive agent research goal.
- Late-batch review (agent_efbcfd31) APPROVE_WITH_P1 → all P1+P2 fixed at
  1179bff (basis-lineage validation on factID grafting, deadlock-safe cascade
  lock order, one-tx dispatch deny, hunt verdict hardened to environment-
  produced evidence, cleanup errors surfaced, profile passthrough, E-only-in-
  hunt). Matrix 35/35 race PASS.

Model budget tonight (GLM-5.3-Flash, all metered): baseline 2 + reviewer 1 +
M4.2 22 + M4.3 15+4 + M4.4 11+2 + M4.5 9+17 = ~83 requests, ~2.1M input
tokens — every session under its preregistered caps; caps never hit. Two D-run
gateway transport failures (unexpected EOF) archived transparently. Key never
printed/stored.

Remaining: optional D/E replication runs when the gateway stabilizes; 09:00
stop (finish minimal unit → CronDelete automation → session summary).
