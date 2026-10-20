# ENG-19 bounded readiness and rollback

The shipped runtime already hard-codes http.get/1, anonymous identity and localhost/loopback-only HTTP, alongside per-contract scope, short grant, task/exploration binding, lease, budget, deduplication, immutable capture and frozen acceptance. A broader administrator contract does not add an adapter or widen this release. Release tests exercise the allowed physical read, then refuse other versions/methods/targets and background outlets. No new runtime flag can disable mandatory guards.

Scoring is an optional recorded proxy-v1 shadow invocation; it does not rank/dispatch. Reviewer is an explicitly host-invoked, metered, same-host advisory session, not an independent authority. Rolling back these optional invocations preserves all mandatory guards. The rollback test runs with and without them and checks unchanged contract identity, denied reviewer authorization, absent-evidence completion denial and real Worker budget denial. No real model or environment effect is used for ablation.

Release status is READY_FOR_APPROVAL: real-model endpoints/versions and budgets have not been approved, so model trials are NOT_RUN. There is no target recall claim, intelligence-quality claim, independent-review PASS, external exactly-once, unknown-environment safety or global convergence proof. Engineering delivery may be synced while review/deployment remains pending. The original package specs/manifests remain unchanged.

Final runner source maps now hash every non-doc Git-visible file, including TypeScript, TSX, browser assets and CI; old receipts keep their original map. Final aggregate logs and build/vet/frontend results must use one frozen runtime map. Documentation-only delivery records do not change that runtime.
