# M0.1 + M1b.2a independent read-only review — report and disposition

Reviewer: separate read-only subagent (agent_c26c0094), 2026-10-18.
Scope: `git diff 177098c..f316a16` (M0.1 scenario registry 7762813, M1b.2a
atomic graph writes f316a16, state 3ce6272). **Verdict: APPROVE_WITH_P1.**

Reviewer independently re-ran: build/vet, harness+agent tests, the offline
Go 1.23 script, TestScenario (19 subtests) — all green; evidence counts and
blob hashes verified as accurate.

## Findings and disposition

- **[P1] lineage boundary check rejected legal inherited parents** — the tool
  layer validates add_intent parents with GetNodeWithSources which allows
  facts/findings from directly related source tasks; the new same-exploration
  check would hard-fail those previously-legal calls. → **FIXED** (c2c1833):
  the transactional boundary check now accepts nodes of this exploration or of
  a direct source task's exploration (live task_relations only), exactly
  matching the tool contract; prove_goal evidence inheritance was confirmed
  unaffected (no boundary check added there, matching old Link semantics).
- **[P2] setFindingCol serialized on the global evidence advisory lock** with
  an uncancellable context. → **FIXED**: plain transaction (new withTx helper).
- **[P2] engine retry slept after the final attempt and retried
  ErrIntentStateConflict blindly.** → **FIXED**: no trailing sleep; CAS
  conflicts break immediately (concurrent adjudication is not transient).
- **[P2] prove_goal reason wording implied all goals were 'met' while
  abandoned goals also settle completion.** → **FIXED**: wording now states
  "no open goals; abandoned goals count as settled".
- **[P2] repeat-proof test claimed but did not assert no version double-bump;
  mirror-failure rollback untested.** → **FIXED (assertion)**:
  content_version stability now asserted; mirror-failure injection remains
  documented as not-verified (no natural injection point without destructive
  schema tricks).
- **[P2] scenario JSON acceptance block uses Go field names** (AcceptanceSpec
  has no json tags). → **ACCEPTED AS-IS**, recorded in DECISIONS: adding tags
  touches the frozen M1a kernel type and all 16 fixtures for a cosmetic gain;
  DisallowUnknownFields guarantees any future tag change fails loudly in tests.

## Clean areas (reviewer confirmed, no issues)

Atomicity of addNodeLinked (true rollback on edge failure); ProveGoal FOR
UPDATE + CAS semantics incl. idempotent already-met and typed ErrGoalNotOpen;
goalless tasks cannot reach HasOpenGoal (tool guard requires a real goal
first); behavior compatibility of all four tool replacements (ids, counters,
notifications, resume side effects); no P0 findings anywhere (no data
corruption path, no lock-order inversion, no SQL injection surface, col/jsonKey
still trusted constants); M0.1 replay genuinely drives the production kernel
with author-written truth constants (no circularity), synthetic/computed
payload expansion has no injection surface (embedded immutable JSON only), and
the offline Go 1.23 module path works with embed.

## Re-verification after fixes

Research PG matrix extended to 18 cases (incl. new
TestGraphOpsInheritedParentFromSourceTask: direct-source parent accepted and
committed, unrelated exploration still rejected) — all PASS with race on
PostgreSQL 17.11 (log sha256
7861cc64b5a6dc694074a0892a8ee91840c6b60281618cba7640ed1ca4441188);
build/vet/agent/harness regressions green.
