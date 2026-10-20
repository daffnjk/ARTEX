# Final source entrypoints

Historical ENG-00 baseline and interface inventory are preserved separately as ENG-00-historical-baseline.json and ENG-00-historical-entrypoints.md. Their line numbers, read-only PG stage and statements of unimplemented work are historical evidence, not current status or instructions. Current acceptance is DELIVERY.json and evidence/final. Runtime source is bound by the full source.json map; do not infer validity from HEAD alone.

| Current path / symbol | Boundary |
|---|---|
| cmd/artex/main.go; server/server.go New | startup, owned lifecycle and exact API routes; Manager.Close drains before stores close |
| server/task_llm.go; agent/planner.go; agent/worker.go | existing default constructors, controlled Planner/Worker assembly and host task/exploration/intent context |
| server/control_contracts.go requireControlAdmin / controlContractChange | signed admin identity; explicit bearer writes; immutable contracts and epoch CAS |
| server/control_budget.go; agent/provider.go transport guard | host attribution, every physical model request reservation, retry and unknown usage accounting |
| db/control_admission.go AdmitAndReserve / CheckExecutionGrant | transaction root, authority/context, cooldown, budgets, dependencies and in-flight dedup |
| db/control_attempt.go; db/control_recovery.go | leased attempt identity, immutable result, cancellation, lost/unknown recovery |
| execution/http.go; execution/supervisor.go | pinned contracted localhost HTTP read; actual request bounds, revoke/cancel and local resource cleanup |
| server/control_runtime.go controlledWorkerTools | single controlled execution tool, no default Shell/browser/MCP/custom/background outlets |
| db/control_observation.go; db/harness_store.go | existing append-only raw host observation store and source lineage |
| db/control_outcome.go CommitValidatedOutcome; harness/control_body.go | frozen criteria, trusted byte verifier and atomic evaluated hypothesis/claim/dependencies/outbox |
| db/control_plan.go; db/control_dedup.go | existing plan stable identity/CAS and explicit dependencies; one-use host retest approval |
| db/control_planning.go; server/control_planning.go | typed feedback, persisted planning windows, authoritative cooldown, optional shadow scoring |
| db/control_close.go; server/control_close.go | reason-specific closed/partial/inconclusive states, draining/handoff and no-model Markdown report |
| db/control_review.go; server/control_review.go | frozen review packet, host-created advisory session, read-only metered model hook, traceable report/evidence |
| db/control_status.go; server/control_status.go | task-bound paginated metadata, admin CAS lifecycle operations, original execution wait and historical download |
| web/src/components/control-task-panel.tsx; web/src/lib/control.ts; existing task detail Overview | honest result/reason/cleanup, budget, execution, evidence and advisory UI |
| db/control_archive.go; db/task_archives*.go; server/task_archives.go | legacy-v3/control-v4 audit, soft retention, new restore identity and quarantined history |
| scripts/test-control-eng00.py; scripts/control-eng00-regressions.json | explicit test DB write probe, raw JSON/exit and immutable per-run receipts; final aggregate selector |
| docs/implementation/control-v1_1/scripts/check_go_test_json.py; acceptance/all.json | original 84-test gate, package pass and no required/subtest skip |
| server/control_e2e_test.go; server/control_release_test.go; .github/workflows/control-validation.yml | real Worker/localhost/PG fault matrix; exact release allowlist and safety-preserving rollback; safe CI |

Legacy entrypoints remain for existing tasks. They do not grant authority to controlled tasks. Only host_http_v1 + http.get/1, contracted localhost/loopback, anonymous identity is locally validated. Real model endpoint versions are not approved by this engineering delivery. Runtime scope cannot be enlarged by model output or this inventory.
