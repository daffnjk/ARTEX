# Session checkpoint — M1a.1 verified; M1b.1 active

Branch research/autopilot-v1; draft PR #2. Main last observed 674dd067eb4eba7dc95b786a63b7576abb064a48. M1a.1 code be0d4e65ba4d382556ac69f65aefc0899f938d80 is saved remotely and imported into the full local Git checkout with exact SHA. Always reread current heads, dirty files, CI, frozen contract and state on resume. No main write, merge, deployment or external scheduler.

M1a.1 passed actual red/green local tests, current Go 1.26.3 scoped CI run 39047329301 and independent Codex code review (receipt 6236232397, no major issues reported). See evidence/M1a-1-ci.json and the preserved local code-blob/log evidence. The CI temporary merge tree is identical to the branch tree; it is not an actual main merge. Read-only trusted binding is complete for this unit, not for the whole database/runtime system.

Current task: M1b.1a schema/API inspection and task-plan persistence. Real norma v0.4.3 has a concrete in-memory TodoStore with private setter; do not invent an SDK persistence interface. Inspect actual agentcore tool registration and task archive lifecycle before integrating. M1b.1b/c cover actual Planner calls, concurrency/restart/delete/archive and isolated PostgreSQL evidence. task-specs/M1b.1.md freezes the unit matrix.

Environment: full checkout /mnt/data/artex-work/ARTEX; local Go 1.23.2, no PG/docker. Direct network workaround is verified GitHub API plus CI artifacts (do not retry the original clone). A bounded one-shot SDK export is requested in existing CI to obtain real Go 1.26.3 and public dependency source; remove sdk-export-request.txt immediately after successful acquisition. No credential or paid fallback. Cost/quota unknown. Live model baseline remains NEEDS_INPUT for approved configuration/budget.

Continue with ready backlog tasks, including M0.1 fixtures and M1b.3a inventory if independent of an active blocker. M1b/M2/M3/M4, real scenario baselines and final readiness remain incomplete. No SQL migration has yet been applied by this session. Keep PR draft and never turn evaluator reports, todo checks or state JSON into write authority.
