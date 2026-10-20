# Local control validation and limited readiness

Use Go >= go.mod (1.26.3; local validated 1.27.0), PostgreSQL17, Python3, Node >=20.9 and the existing npm lock. Do not downgrade go.mod or replace the database. No production configuration is provided by this delivery.

## Repeat engineering acceptance

Set both ARTEX_PG_DSN and ARTEX_TEST_PG_DSN to the same explicitly disposable localhost database named artex_research_test*. The runner requires a password-free local development connection; it prints identity and probes table-owner DDL/DML in a rolled-back transaction. Do not point it at a real target or production database. It never falls back to application configuration.

```sh
python3 scripts/test-control-eng00.py /private/tmp/artex-control-fresh-receipts all
```

The destination must not exist. The runner records commands, raw exits, go test JSONL, stderr, environment identity, all non-doc Git-visible file hashes, acceptance and checker hashes. Both gates must PASS: 84 required roots and the fixed affected-regression inventory. Missing roots, required/subtest skips, package failure, build failure or nonzero original exit fail acceptance. This is deterministic local integration, not real-model behavior evaluation. Compare source maps before resuming historical receipts.

```sh
go build ./...
go vet ./control ./execution ./db ./agent ./server ./harness ./evidence ./report
cd web
npm ci --ignore-scripts --no-audit --no-fund
npm run check
npm run build
```

Go tests require the explicit matching test DSNs even for packages whose TestMain accesses PG. Do not assume a seemingly pure test is offline. The final delivery also runs full affected package suites with -race; consult their raw results separately from the required-root gate.

## Local UI display

```sh
python3 scripts/control-ui-fixture.py --output /private/tmp/artex-ui-fresh --seconds 600
```

This owns a bounded Next dev child and localhost-only display backend. Use its printed unique .localhost URL; `/api/ui-fixture-bootstrap` seeds only an invalid synthetic display token and redirects to task1600. It does not authenticate against ARTEX or accept terms, access a real DB/model/target, or prove backend authorization. It refuses runtime writes. Ctrl-C/timeout stops its child. Synthetic evidence downloads are not implemented; real task-bound evidence downloads are verified by Go integration tests. Screenshot self-check is not independent review.

## Application startup/configuration

Build `go build -o /private/tmp/artex-control ./cmd/artex`. For a deliberately isolated development instance, supply its own ARTEX_PG_DSN and empty data/config directory, then start with `-addr 127.0.0.1:18787 -proxy '' -data /private/tmp/artex-control-data`. Never reuse a production config/profile or default shared data directory. This command is a startup recipe; no production service was started or reconfigured by this work. Configure model providers only after endpoint/version/budget approval. No real-model release approval is included.

The existing single administrator signs HS256 ARTEX-subject expiring sessions; no new multi-tenant ACL is claimed. Task detail -> Overview shows immutable contract version/epoch, budgets, outcome/reason/cleanup, original executions and host evidence links. Manual contract approval requires explicit bearer and current epoch; a cookie alone cannot authorize writes. A contract is necessary, never sufficient: only http.get/1 over contracted localhost/loopback with anonymous identity is executable. Shell/browser/MCP/custom/background outlets stay closed for controlled tasks. Legacy tasks remain legacy; research scope or model text is not approval.

Wait/collect references the original execution; WAIT_EXPIRED does not create work. Revoke uses current epoch and blocks future local starts. Existing requests may already have effects. Cancel waits only for actual local supervisor cleanup. Unknown effects retain budget reservations and require separately authorized reconciliation; do not retry them under a new ID. Close/handoff records partial/inconclusive and unconfirmed cleanup honestly, with host-only Markdown report and authenticated evidence links. A direct answer does not expand to asset coverage. Reports never need a final model call.

## Migration, archive and rollback drill

Startup schema uses the existing PG advisory lock; the migration-twice test preserves approved digest/epoch. Archive v4 retains control audit and files without disabling append-only triggers. Controlled delete is a hidden soft delete; audit is retained. Restore uses a new paused task/exploration, revoked permission, no live executions/claims/plan and explicit source-reference mapping. Download `/api/tasks/{new_id}/control/restored-history`; inspect the archive warning for new_id. Cold files remain retained, not installed as a live new workspace. Fresh contract, plan and frozen acceptance are required. This release does not reclaim historical control storage automatically.

Disable optional score/review invocations to roll them back. Keep mandatory authorization, lease, budget, evidence, deduplication, cooldown and close gates. If a runtime regression appears: stop admitting new tasks, revoke current task contracts, collect original results, record unknown effects/cleanup and retain all audit. Roll back application readers only; do not drop additive tables or restore old reusable grants. Older readers cannot safely restore controlled v4 packages. Do not merge/deploy or perform production migration based on this local engineering result.

## Unapproved trials and review

Real model trials are NOT_RUN: no endpoint/model version/budget was explicitly approved. A future bounded trial must use synthetic data, the same metering/safety base, record all physical requests/tokens/unknown reservations and compare ground truth where available. No true-target recall or real-model benefit is claimed. Reviewer remains same_host_advisory, never an independent approval. Independent implementation/integration/release review is pending.
