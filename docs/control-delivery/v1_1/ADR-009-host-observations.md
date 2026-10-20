# ENG-09: host capture into the existing harness store

Only the host's committed, digest-checked HTTP result can produce controlled observations. IDs bind task, exploration, execution, attempt, lease and context. The legacy observation writer refuses controlled tasks. Immutable response results and append-only observations prevent replacement; identical capture retries are idempotent.

The first enabled method caps each response at 64 KiB, below the existing harness BYTEA limit. All enabled responses therefore use the existing bounded payload storage, atomically with capture metadata. Large-object adapters remain disabled; this ticket does not introduce a second artifact store or claim to test large-object GC. Raw headers stay in the trusted record; the model view removes them and redirect Location, retaining only content type/length. Truncated data remains incomplete and invalid; identity rejection or stale context is unknown, never a valid negative.

Migration is additive. Rollback disables the controlled capture bridge; it must not permit legacy string evidence to bypass controlled lineage. Independent review is pending.
