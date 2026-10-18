# Session checkpoint — M1b.1 / M0.1 / M1b.2a complete; next M1b.2b

Branch research/autopilot-v2 (local only, 10 commits ahead of cc8acc9d):
24a6337+9f37f2e+177098c (M1b.1 plan persistence & CAS), d12f2ff (planner
integration), 7762813 (M0.1 scenario registry), f316a16+c2c1833 (M1b.2a atomic
graph writes), plus state commits. Working tree clean. Always compare real Git
heads, dirty files and frozen state before resuming.

Verified this session on isolated local PostgreSQL 17.11 (artex_research_test,
runner scripts/test-research-pg.sh — now 18 cases incl. graph-ops): CAS matrix
(row identity, no resurrection, ABA), scenario replay (16 scenarios/19 subtests
through the production kernel), atomic lineage writes with boundary semantics
aligned to GetNodeWithSources inheritance. Two independent read-only reviews
(agent_fdb4fd4e, agent_c26c0094) both APPROVE_WITH_P1 — every P1/P2 fixed and
re-verified; reports in docs/autopilot/evidence/.

NOT verified / not claimed: CI research-postgres job (added, NOT_RUN until
push), full LLM-loop end-to-end, M0.2 live-model baseline (NEEDS_INPUT: no
approved model credentials/budget), UI.

Remote save: NOTHING pushed — no gh auth, user chose local-only. All commits
exist only in /Users/admin/Documents/渗透测试/ARTEX (loss risk until push).
On auth: push branch, open draft PR, confirm CI (incl. new research-postgres
job), then close the CI-NOT_RUN entries in evidence.

Next tasks: M1b.2b (reconciliation / rollback / cancellation idempotency /
unknown-effect accounting — design from the inventory in task-specs/M1b.2a.md,
C7/C9/C10 notes are the seeds); M1b.3a completion-path inventory is
independent and can interleave. State files are the scheduling truth; do not
treat checkboxes as completion evidence.
