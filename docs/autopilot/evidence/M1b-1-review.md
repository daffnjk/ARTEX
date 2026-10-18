# M1b.1 independent read-only review — report and disposition

Reviewer: separate read-only subagent (agent_fdb4fd4e), 2026-10-18.
Scope: `git diff cc8acc9d..9f37f2e` (storage 24a6337, planner integration d12f2ff,
wrap-up 9f37f2e). Format: P0/P1/P2 per docs/autopilot/prompts convention.
**Verdict: APPROVE_WITH_P1.**

## Findings and disposition

- **[P1] CAS INSERT branch did not validate the expected revision** — a stale
  in-flight writer (expectation N>0) against a deleted row (e.g. purged by
  archive) would resurrect a plan via INSERT as revision 1, and that dirty row
  would survive restore. → **FIXED**: SaveTaskPlan rewritten to a three-step
  form (CAS UPDATE matching `(revision, row id)`; INSERT only for the (0,0)
  first-write expectation with ON CONFLICT DO NOTHING; typed conflict probe —
  absent row ⇒ PlanConflictError{HaveRevision:0}, never a resurrection).
- **[P1] revision-reset ABA** — delete→recreate starts at revision 1, so an old
  writer holding (1, oldRowID) could silently overwrite the new row. →
  **FIXED**: TaskPlan carries the storage RowID; the CAS predicate matches
  `revision AND id`, a recreated row has a new id, colliding revisions cannot
  match. New tool message distinguishes "row deleted/archived".
- **[P2] DSN query-parameter bypass** — pgx applies `?host=`/`?database=`
  overrides after the URL authority, so `…localhost…?host=prod` passed both
  guards. → **FIXED**: runner script and in-test guard now reject any DSN
  containing `?` (negative-tested: all three guard rejections verified).
- **[P2] second suite could silently SKIP yet print PASS** — the archive
  regression skips when PG is unreachable. → **FIXED**: runner captures the
  log, fails on test failure AND on skip markers; dedicated runner never
  accepts a silent skip.
- **[P2] missing matrix cases** — no "expectation>0 with row absent" and no
  "delete→recreate ABA" case existed. → **FIXED**: added
  TestTaskPlanPGStaleWriteAfterRowDeleted, TestTaskPlanPGDeleteRecreateRejectsABA,
  plus TestTaskPlanPGConcurrentSameRevisionUpdate (true concurrent same-(N,R)
  double write: exactly one winner).

## Clean areas (reviewer found no issues)

SQL parameterization; concurrency assumption (per-task single planner loop
goroutine, engine.go:763 + settling gate; non-concurrent tool execution in the
harness); fail-closed completeness; legacy-mode byte-compatibility (worker/
chat/mainagent scratch todos untouched); archive purge placement and restore
whitelist absence; schema FK/trigger semantics; test assertion honesty (typed
conflict assertions, data-integrity assertions); CI job structure (and the
review confirmed the diff incidentally fixed a digit-transposed upload-artifact
pin `…4886f43607fa02` → correct v4.4.4 `…4886f3607fa02` that would otherwise
have failed CI resolution); prompt-injection surface smaller than the legacy
path (bounded content, fixed templates, basis/expect never re-injected).

## Re-verification after fixes

PG matrix extended to 11 cases + archive regression — all PASS with race on
the same isolated PostgreSQL 17.11 (log sha256
dd2f451ab47ed57027d53676c587a04f6245c1c5c06012559abce8f55ffaefcc); guard
negative tests reject query-param DSN, non-local host, and wrong db name; full
regression battery green (build, vet, agent/db unit, race harness/cmd,
TestGoalCompletion). One intermediate single-statement SQL attempt (INSERT…
SELECT…WHERE guard) was itself caught by the matrix (normal updates turned
into conflicts) and replaced — the failure evidence is preserved in this
session's log history, not hidden.
