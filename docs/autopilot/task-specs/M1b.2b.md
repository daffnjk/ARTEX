# M1b.2b — Reconciliation, idempotency and unknown-effect accounting

Predecessor: M1b.2a (f316a16/c2c1833). Read-only inventory completed 2026-10-18
against c5ede8b (Explore agent report; key findings folded in below).

## Inventory conclusions (what already works)

- Startup recovery: ResetRunningIntents, task lifecycle reload + first planner
  kick via engine.Run, archive job recovery (the most complete pattern:
  failed→manual retry), finding-retest interruption, side-question interrupt,
  notification deliveries (lease-based, self-healing, 3 attempts), P3 trigger
  watermarks. No network/exec inside DB transactions (re-confirmed).
- Planner wake loss is bounded by the heartbeat timer (default/lower bound
  600s) and restart already kicks a first round via engine.Run — acceptable;
  no new channel added.

## Gaps addressed in this unit

1. Historical partial records (pre-M1b.2a split writes) + ongoing CancelIntent
   race products: read-only `db.GraphIntegrityReport` covering seven checks —
   fact/intent/goal/finding missing their lineage inbound edge, met goal
   without proves edge, findings row pointing at a broken node, and dangling
   tool_use activities without tool_result pairs. Reports counts + bounded
   findings; repairs nothing (repair decisions are human/later milestones).
   API/UI exposure deferred to M2.3 read-only inspection surface.
2. Unknown-effect accounting: dangling tool_use pairing gives the "effect
   unknown" ledger — restarting a run must surface these as unknown-effect
   context, never blind-replay (recorded decision; wiring into worker context
   belongs to M2/M3 runtime).
3. Repeat-cancel idempotency: cancelling an already-soft-deleted intent is now
   an explicit success no-op at the control layer (db layer stays strict —
   SoftDeleteIntent still rejects non-live states; API absorbs the repeat).
4. Legacy no-edge intent source closed: fallbackChat human intents now use
   AddIntentWithParents (node + root lineage edge atomically).

## Recorded decisions (not changed here)

- Finding dedup keys (findings/RecordFindingTx are pure INSERT, no natural
  key): behavior change deferred to its own milestone; the integrity report
  does not treat duplicate findings as breakage.
- Commit-failure injection for the big transactions: no natural injection
  point without destructive driver tricks; covered by defer-Rollback + CAS
  semantics + the error-surfacing matrix. Recorded NOT_TESTED, honestly.
- ControlWork-timeout→paused→re-cancel chain and hard-delete race (CancelIntent
  snapshot vs concurrent addNodeLinked): engine-level tests deferred with the
  server test harness; the report's check 1/2 will surface any resulting
  orphans, which is the reconciliation net.

## Acceptance

- PG tests: each seeded orphan class is detected (and counted); a clean
  exploration yields zero findings; dangling tool_use detection round-trips;
  repeat soft-delete at the db layer still errors (strictness preserved).
- Regression: full suites stay green; runner/CI re-run.
