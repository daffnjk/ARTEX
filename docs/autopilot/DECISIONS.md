# Development decisions

## 2026-10-17 / GOV.0

- Live main is 674dd067eb4eba7dc95b786a63b7576abb064a48. Existing research/evidence-harness-m1 is behind by one merge commit and ahead by zero, with an identical tree. Created research/autopilot-v1 from the merged baseline; no unmerged work discarded, old branch unchanged.
- Root AGENTS.md returned 404; recursive tree contained no AGENTS.md or autopilot files. Do not overwrite unrelated instructions.
- Direct local git clone failed at DNS. Prefer the connected GitHub API and an artifact of committed source from the existing, read-only, time-bounded research workflow. No new service, credential scope or paid fallback. A source bundle contains Git objects only, not checkout credentials or untracked environment files. It is an acquisition/provenance aid, not a test result.
- Freeze the owner's finite scope and authority in PROJECT_CONTRACT.md. Dependency splitting does not remove parent requirements. State records are scheduling hints, never completion evidence or runtime authorization.
- State cannot contain its own eventual commit hash. checkpoint_parent_commit identifies the observed predecessor; git log identifies the state-file commit; subsequent checkpoints link verified code commits and immutable evidence. Never treat an untested metadata update as code validation.
- M1a.1 is first: current evaluation.go chooses the verifier by request criterion/version. No current dynamic reproduction or fix is claimed yet. Missing trusted config must fail closed. Database integration remains M2.

## 2026-10-17 / M1a.1

- Exact request-to-frozen-spec matching is deliberate. Model semantic matching would widen authority; a changed experiment/context needs a newly trusted frozen binding rather than requester choice.
- Retain the old constructor for empty-registry integrity/CLI compatibility, but an unbound nonempty registry is now fail-closed. Host callers use NewEvaluatorWithAcceptance; the CLI does not ingest goal specs.
- Reread after verification and discard changed state. This is not a database transaction or defense against changes after the return; final-write CAS remains a mandatory M1b.3/M2 dependency.
- Retained the pre-fix dynamic failures and all existing assertions. Real current-code project CI and independent review are separate from successful local stdlib tests.
- .gitignore ignores new docs globally; explicitly stage only these reviewed autopilot records with git add -f, without weakening repository ignore rules.

## 2026-10-17 / M1a.1 acceptance and M1b.1 preparation

- be0d4e65 passed current scoped Go 1.26.3 CI and separate Codex read-only code review (receipt 6236232397). The agent CI tested temporary PR merge 60dedbff, whose tree was checked equal to the branch commit; main was not merged or modified.
- Import API-created Git objects only after reproducing the full exact SHA. The API normalizes dates to UTC but this commit retains +0800 raw headers; no history or author information was invented or rewritten.
- norma v0.4.3 tool/todo.go (blob e574768836ed068becd3e699fe1140cf59ae615a) exposes concrete TodoStore/List/Tool, private set, and content/status only. Do not invent a pluggable persistent-store API; inspect agentcore registration before integration. Todo status remains a model note, never an acceptance record.
- Acquire only the project's real public Go toolchain and vendored dependencies through the existing read-only 15-minute CI job, once. No unrelated cache, credentials or environment dump; compressed artifact capped at 160 MiB, retention one day. The request marker must be removed after successful acquisition. This neither adds a paid service nor changes budget limits; cost/quota remain unknown. Acquisition failure remains explicit.
