# M1a.1 — Trusted frozen goal acceptance binding

Status: IN_PROGRESS. Baseline code: 674dd067eb4eba7dc95b786a63b7576abb064a48; working source: aeb693f6d0b90575eab5e97bb68c6d0f28220da6; branch research/autopilot-v1. Follow-up to PR #1 review 4301953152.

## Actual path and proposed change

`harness.Evaluator.Evaluate` checks request/evidence, then currently selects a verifier by request CriterionID/Version. The offline CLI constructs an empty registry. `agent.AugmentTools` installs a separate optional completion-review wrapper; it does not call this evaluator or authorize its reports.

Add a read-only host-supplied AcceptanceResolver, looked up only by TaskID/GoalID, returning a versioned AcceptanceSpec for an exact claim, criterion, experiment and context. Provide an immutable in-memory frozen source for host-built offline contracts and a distinct constructor for configured evaluators. This is real core dispatch integration, not a PostgreSQL resolver or a common completion gate. Do not expose a request/CLI path that installs acceptance specs.

The legacy constructor remains source-compatible but cannot invoke registered verifiers without a trusted acceptance source. The empty-registry CLI keeps REVIEW/criterion_unimplemented/exit 2. Additive acceptance_version is optional for integrity-only requests, mandatory to match a configured frozen spec. No schema, authority or legacy/review wrapper behavior change.

Bind all fields before verifier selection, then reread current acceptance after verification. A changed/missing/error/cancelled state discards the verdict. Exact text matching fails closed; no semantic claim widening or model confidence. This read-only check is not an atomic completion guarantee: M2/M1b.3 must revalidate and commit under database concurrency control, including after this method returns.

## Required tests (frozen before implementation)

- Dynamic baseline regression: a registered synthetic weak verifier must not be called or confirm without trusted goal state, including an unrelated claim.
- Correct frozen task/goal, acceptance revision, criterion/version, claim, experiment and context dispatch exactly the corresponding fixture verifier; an unrelated registered verifier is never called.
- Each mismatch, missing goal, wrong ownership, invalid configuration, resolution error/panic and cancellation fails closed. Version changes during resolution/verification cannot confirm.
- Frozen-source and registry inputs are copied; concurrent reads are safe. Snapshot changes, including same-version content changes, are detected (the real store must also prohibit revision reuse).
- Existing evidence integrity/bounds/binding, verifier errors/panics, verdict validation and legitimate different control identities remain tested; adjust host fixtures, not assertions, for the stronger constructor contract.
- Actual offline core/CLI test/race/vet/build/review-exit checks and current project-toolchain CI, plus existing TestGoalCompletion regressions. Record baseline and red failure before the fix.

## Migration, rollout, rollback and review

No SQL migration or execution capability. Configured library callers must provide host-owned frozen specs; existing unbound registries become REVIEW intentionally. Reverting only this commit would restore the reviewed criterion-substitution risk; it is not a safe way to retain domain-verifier confirmation. Unknown or unbound requests must remain REVIEW. Database trust, final transaction/CAS, capture authenticity and all-path completion are explicitly not claimed.

Independent review uses a separate read-only reviewer when available. Self-review is labeled self-review. Final acceptance requires actual test evidence, not PR existence, model approval, JSON state or a source hash alone.
