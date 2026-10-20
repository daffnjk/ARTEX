# ENG-16 status UI

Reuse the existing single-admin identity. Task-bound read-only paginated snapshots omit secret references, raw grants and bodies. Writes require explicit bearer and expected-epoch CAS; cookies remain read-only. Wait collects the original execution; cancellation acknowledges only an actual local supervisor. Partial/unknown/unconfirmed remain visible.

Existing task details now expose budgets, lifecycle, host evidence links and advisory trust domain. Shell/browser/MCP/custom execution stays closed. Screenshots use a bounded synthetic localhost display backend, no real login/model/target; they are self-checks, not independent review.

The original frontend check baseline had 134 errors. Existing Biome rules and dependencies remain: format/import fixes, documented best-effort catches, explicit async calls, linked labels and native keyboard controls repair the baseline. LLM record request keys prevent stale responses. Around 35 existing files were necessarily touched; Git contains the complete reviewable diff. No unrelated dependency upgrade or relaxed checks.

Evidence: ENG-16 runs/05 has 3 required + 93 regressions PASS. frontend-final-01 check/build exit 0 (original check baseline exit 1). ui-03 contains screenshot, AX and GET-only request receipt. The synthetic PNG is committed. Independent review and real-model evaluation remain pending. Roll back application readers only; preserve additive schema and audit history.
