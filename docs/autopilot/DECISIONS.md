# Development decisions

## 2026-10-17 / GOV.0

- Live main is 674dd067eb4eba7dc95b786a63b7576abb064a48. Existing research/evidence-harness-m1 is behind by one merge commit and ahead by zero, with an identical tree. Created research/autopilot-v1 from the merged baseline; no unmerged work discarded, old branch unchanged.
- Root AGENTS.md returned 404; recursive tree contained no AGENTS.md or autopilot files. Do not overwrite unrelated instructions.
- Direct local git clone failed at DNS. Prefer the connected GitHub API and an artifact of committed source from the existing, read-only, time-bounded research workflow. No new service, credential scope or paid fallback. A source bundle contains Git objects only, not checkout credentials or untracked environment files. It is an acquisition/provenance aid, not a test result.
- Freeze the owner's finite scope and authority in PROJECT_CONTRACT.md. Dependency splitting does not remove parent requirements. State records are scheduling hints, never completion evidence or runtime authorization.
- State cannot contain its own eventual commit hash. checkpoint_parent_commit identifies the observed predecessor; git log identifies the state-file commit; subsequent checkpoints link verified code commits and immutable evidence. Never treat an untested metadata update as code validation.
- M1a.1 is first: current evaluation.go chooses the verifier by request criterion/version. No current dynamic reproduction or fix is claimed yet. Missing trusted config must fail closed. Database integration remains M2.
