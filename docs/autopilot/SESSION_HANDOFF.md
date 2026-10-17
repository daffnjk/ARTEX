# Session checkpoint — real SDK ready; M1b.1a awaiting PostgreSQL

Branch research/autopilot-v1, draft PR #2. Main last observed 674dd067eb4eba7dc95b786a63b7576abb064a48. Code unit M1a.1 be0d4e65 is verified/reviewed; later checkpoint 46c5798b passed CI 39048395920. Always compare real Git heads, dirty files, CI and frozen state before resuming. No main write, merge or deployment.

Verified real Go 1.26.3 and norma v0.4.3/dependencies were acquired from artifact 12106468826. Root module files are unchanged. Actual local core race and TestGoalCompletion checks pass; evidence/ENV-SDK-acquired.json records hashes and scope. The local vendor symlink is a build aid outside versioned source, not a replacement SDK. SDK request marker removed.

Current M1b.1a business-code edits remain LOCAL/UNCOMMITTED at this environment checkpoint: task plan types, storage/CAS, task-owned revision fence, versioned archive snapshot/restore, validation and explicit real-PG tests. They are not yet accepted; unit/race and archive-file tests alone do not prove SQL behavior. Inspect and preserve dirty files in /mnt/data/artex-work/ARTEX. The next operation is actual isolated PostgreSQL validation, repair and a code-unit commit, then real Planner integration.

A one-shot public postgres:16-alpine image export is requested through existing bounded read-only CI. It exports a newly created but never-started image container, not a database or runtime volumes. Only existing executor capabilities may be used. Remove pg-export-request.txt on successful acquisition. No model or target calls, permission increase or paid fallback. Cost/quota unknown. Live model baseline remains NEEDS_INPUT.

Do not stop after this checkpoint: continue ready dependency tasks, including M0.1 and M1b.3a when independent. M1b/M2/M3/M4 and final readiness remain incomplete. No production migration or deployment. State files and planning checkboxes are not completion evidence or runtime authority.
