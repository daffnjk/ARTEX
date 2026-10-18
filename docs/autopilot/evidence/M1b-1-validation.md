# M1b.1 validation — task plan persistence & planner CAS lifecycle

Date: 2026-10-18 · Branch: `research/autopilot-v2` (local only, not pushed) ·
Commits: `24a6337` (M1b.1a storage) + `d12f2ff` (M1b.1b planner integration)

## What was implemented

Planner plan scratchpad promoted from a process-local `map[int64]*TodoStore` to
authoritative PostgreSQL storage with revision CAS; hydration fail-closed at
round start; persist-before-ack TodoWrite on the real tool-registration path;
explicit conflict for stale/concurrent writers; archive purge follows the
enumerated exploration tables; dedicated non-skipping isolated-PG test runner.

Design decisions and the SDK binding-point analysis (norma v0.4.3: TodoStore
exports only List(), agentcore appends its own TodoWrite when Options.Todos !=
nil and the registry lets the later same-name tool win, reminder path is
nil-guarded) are recorded in `task-specs/M1b.1.md` and `DECISIONS.md`.

## Verification (all PASS, real local PostgreSQL 17.11 `artex_research_test`)

- PG matrix 8/8 (race on): migration-twice idempotent (data survives re-Open),
  roundtrip + revision increments, concurrent first-write (exactly one winner,
  other typed conflict), stale write rejected without last-write-wins,
  isolation + cross-task basis rejection, delete cascade, closed-handle errors
  surface (no false success), archive purge removes plan and post-archive
  hydration is an explicit empty plan.
- Existing archive round-trip regression re-run PASS under the same DSN.
- Agent unit 6/6: persist-before-ack ordering visible via run state + ack
  revision, explicit conflict message names current revision, validation caps
  reject before persistence, store error states "plan NOT saved", hydration
  error propagates (round stops), stable step IDs across calls.
- Regression: `go build ./...`, vet, race `./harness ./cmd/harness-eval`,
  `^TestGoalCompletion ./agent`, full `./agent` package — all PASS.

## Honest boundaries (NOT_RUN / deferred)

- CI `research-postgres` job: workflow written and YAML-validated; the job has
  NOT run (branch not pushed — user chose local-only commits this session).
- Full-LLM-loop end-to-end planner recovery: storage- and tool-level semantics
  verified; running a real model round is deferred to M2.3/M3 integration and
  is not claimed here.
- Plan-status (completed) is a model note; no acceptance/completion path reads
  `task_plans` (by construction and grep; M1a.1 binding untouched).

## Runbook

Migration, verification commands, rollback (with pg_dump preserve + explicit
DROP), and known limitations: `docs/research-harness/plan-persistence.md`.
