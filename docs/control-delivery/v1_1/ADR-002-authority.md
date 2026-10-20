# ADR-002: immutable authority, task-root serialization

Control contracts and contract events are append-only under existing forbid_mutation triggers. Their task IDs are historical identifiers without cascading FK deletion: deleting a task never deletes authority history, and a restored/new task must obtain new approval. The active pointer and epoch live on tasks; approve/revoke lock that row, compare expected epoch, update pointer/epoch and append the event in the same transaction. Controlled execution will use the same lock order; no request runs in these transactions.

Approval is a host-only storage API, not an agent tool. The staged HTTP handler gets its principal from a private context key installed only after HS256 JWT verification, mandatory expiry and the existing single-admin ARTEX subject. User JSON cannot supply actor or role; unknown fields are rejected. A valid token for a model/worker/ordinary subject is still denied. The repository has a single administrator, so all tasks belong to this administrator; no invented multi-tenant role system is added.

The handler is intentionally not mounted until ENG-07 wires the controlled executor. Legacy tasks with first_run_at set cannot be relabelled by approval. Old tasks retain mode=legacy. Coverage AddAutoScope/AddAgentScope never changes contracts. Existing task APIs and terminal status vocabulary are unchanged.

Migration is additive and uses existing embedded schema/migration lock. Repeated opens are tested in the fixed regression inventory. No approval history or trigger is removed on rollback; do not expose staged routes if execution integration is incomplete.
