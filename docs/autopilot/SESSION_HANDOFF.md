# Session checkpoint — night 2026-10-18/19: research matrix closed through M4.2; FINAL checklist remains

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

Model budget: GLM-5.3-Flash only, metered hard caps; tonight's totals: baseline
2 runs + reviewer 1 call + M4.2 pilot 22 requests/372k tokens (all under
preregistered caps; no cap ever hit). Key never printed/stored.
