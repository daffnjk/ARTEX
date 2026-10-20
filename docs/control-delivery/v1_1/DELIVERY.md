# ARTEX control v1.1 engineering delivery

Status: **LOCAL_VALIDATED_SYNC_PENDING**. ENG-00 through ENG-19 are verified_local; independent review remains needs_independent_review. This is engineering validation, not merge, deployment or real-model approval. Structured states and actual receipts: [DELIVERY.json](DELIVERY.json), [release-readiness.json](release-readiness.json).

Final runtime map SHA-256: `063a50d68790856024e7b9857d200b1af058ac233b6b4405ecf44195dbf508b9`. It covers all 749 non-doc Git-visible files, including Go, frontend and CI. Documentation/checkpoint metadata can advance without changing that runtime map. Final code checkpoint and remote synchronization are recorded after push.

| Final verification | Actual result |
|---|---|
| acceptance/all.json | 84 required roots run once and PASS, original go exit 0, required packages PASS, no required/subtest skip |
| Fixed regression inventory | 116 entries PASS, original exit 0 |
| Full affected package suites, -race | 621 roots PASS, exit 0, no failure; optional real-model TestLiveContextReview NOT_RUN/skip; report has no tests |
| go build ./... | exit 0 |
| go vet control/execution/db/agent/server/harness/evidence/report | exit 0 |
| Existing npm run check / npm run build | both exit 0, rules and dependencies unchanged |
| Additive migration, archive/restore, deletion/late result, cancellation | final-tree Go integration PASS |
| Packet/preflight/runner unit checks | exit 0; metadata checks do not substitute for Go tests |
| Remote safe CI | NOT_RUN before push; actual result recorded subsequently |

Final aggregate runs/06 and full-package extra-04 have identical runtime maps. Their selected original JSONL/stderr/commands/exits/hashes are committed under [evidence/final](evidence/final/MANIFEST.json); JSONL files are gzip compressed losslessly, with raw and compressed hashes. All earlier failures and full raw local histories remain in the original ignored reports directory. They were not replaced by stitched green results.

ENG-16 screenshot self-check: [synthetic partial-result UI](ENG-16-ui.png). It shows partial, lost/unknown, unconfirmed cleanup, invalid incomplete evidence and same_host_advisory identity. Collecting references the original execution; the display fixture request log contains no POST. This is not independent UI review. Real admin/task/revision behavior is tested against Go middleware and PG.

Full-package testing caught and repaired compatibility issues beyond the fixed list: legacy archives keep writing v3 while controlled audit uses v4; bounded global integrity lists deterministically show newest issues without deleting historical counts; new archive fixtures explicitly initialize the same LLM tables as the real server; Manager.Close now drains owned task/persistence writers before closing stores. Host closure remains separate from a cancelled operation context. The original assertions remain, and the previously failing relevant tests are added to the fixed inventory. One broader retry used a fresh database because an old fixed-CIDR company fixture persists global scope across runs; original databases and failed logs remain.

The first operational scope remains contracted anonymous localhost/loopback http.get/1. A broad contract cannot enable other adapters. Shell/browser/MCP/custom/background execution stays closed for controlled tasks. Scoring is proxy-v1 shadow only; Reviewer is same-host advisory with host session identity and metering, never an approval. Dropping optional invocations does not disable mandatory authorization, budget, evidence, deduplication, cooldown, lease or completion gates.

Known limits: no approved real-model endpoint/version/budget, no real-target trial, no production migration/deployment, no independent final review. Real-model efficacy and cost comparison are NOT_ESTABLISHED; true-target recall is not claimed. Only deterministic body SHA256 comparison is installed; business criteria need manual review. Unknown effects retain reservations, cleanup confirmation covers local resources, and external exactly-once/global convergence/unknown-environment safety are not claimed. Controlled archive retains hot audit and cold files; restore quarantines history under fresh task/exploration IDs and requires new approval, plan and acceptance. No automatic historical purge or hot-store reclamation is included.

Startup, configuration, repeating acceptance, cancellation, migration and rollback: [RUNBOOK.md](RUNBOOK.md). Architecture choices and integration self-check are in ADR-001 through ADR-019. Original package manifests and historical reports are preserved. No production configuration, repository visibility, default branch, merge or deployment was changed.

Remote: `https://github.com/daffnjk/ARTEX.git`. Dedicated branch: `feat/artex-control-v1-1`. Normal branch-only push is authorized; no force push or default-branch push. Existing release workflow triggers only v* tags, research workflow push only research/evidence-harness-m1; the new delivery workflow runs synthetic localhost PG tests with contents:read and no production secrets/deploy. Independent reviewer must assess this delivered branch; implementation self-check does not sign release approval.
