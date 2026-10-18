# M1a.1 implementation and validation checkpoint

Date: 2026-10-17 UTC. Branch: research/autopilot-v1. Baseline/source before code edits: `aeb693f6d0b90575eab5e97bb68c6d0f28220da6` (business code identical to main 674dd067). Exact tested new code blobs are in M1a-1-local-results.json; the containing Git commit and subsequent CI/checkpoint bind them to this branch. No own-commit hash is fabricated in this file.

## Source acquisition

GOV.0 CI run 39045462900 completed success for offline-core job 117433175731 and agent-integration job 117433175854, on aeb693f6d0b90575eab5e97bb68c6d0f28220da6. Artifact 12105736722 contains exact committed-source bundle and scoped test log. Download SHA-256: 66ea54a3d820a116a053268712689cb9704732e1d12beb9b5b341ee1c237593c, matching GitHub's digest. Bundle verification and git fsck --full passed; head/tree match aeb693f6 / f8b4f7cec553811d835a685ff62461e1fcb96ae6. The full-history checkout was clean before this unit. This is not current-change CI proof.

## Red → green evidence

The original offline script passed 15 top-level tests (50 including named subtests). Before editing production code, added acceptance_regression_test.go and reran: exit 1; both the request-selected weak criterion and same-criterion unrelated claim reached the synthetic verifier and returned confirm/supported. Preserved diagnostic for each:

```
unbound request reached verifier: called=true disposition=confirm conclusion=supported
```

After the fix, the same regression and the expanded frozen-binding matrix passed: 25 top-level tests, 90 including named subtests, zero failures. The existing offline script actually ran race tests, vet, CLI build and the intact fixture; the fixture remained review/criterion_unimplemented with exit 2. Harness statement coverage 99.4%, CLI 75.0%; neither coverage nor a hash proves the proposition or system security.

The first post-fix command printed script exit 0 but the executor wrapper reported status 1. This ambiguity was not counted as a pass by itself: a clean independent rerun returned process exit 0, then the final expanded matrix returned process exit 0 again. All local raw logs remain under the session results directory; stable hashes, counts, exact command and code blobs are checkpointed in M1a-1-local-results.json. Remote CI will retain its own raw log/source artifact for three days. No secrets/target data were used.

Root-module test attempt with GOTOOLCHAIN=local GOPROXY=off failed before compilation because installed Go is 1.23.2 while go.mod requires 1.26.3. Root go.mod/go.sum were not changed. The isolated offline test is not a full ARTEX or real-SDK/PG validation; current-change project CI is pending at this checkpoint.

## Self-review and scope

Selection now comes only from a host-installed task/goal source after exact acceptance revision, criterion/version, claim, experiment and context match. No request can install that source. A source failure, panic, invalid/mismatching state or change during verification discards the verdict. Unbound registered evaluators fail closed; the empty-registry CLI retains its prior REVIEW behavior. Immutable host-source/registry copies, cancellation, missing registered rule and legitimate different control identities are tested. Existing assertions were retained; mutation testing now also verifies the checker was actually invoked.

No schema migration, runtime execution/tool capability, authorization change or automatic completion was introduced. No real database resolver or atomic completion guarantee is claimed. M2/M1b.3 must enforce authenticated source reads and a final revision/condition check inside the actual database write transaction, including changes after Evaluate returns. Revision reuse is forbidden by the source contract, not magically prevented by this read-only core.

Independent review: not yet run at this checkpoint; self-review above is not independent review. Full system tests, PostgreSQL, UI, end-to-end local scenarios and capability gains remain unverified. Rollback/caller migration are described in task-specs/M1a.1.md. Reverting restores the reviewed defect; it does not preserve the new binding guarantee.
