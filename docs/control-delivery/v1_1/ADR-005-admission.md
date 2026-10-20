# ADR-005: atomic admission, frozen grants, recoverable dispatch intent

AdmitAndReserve serializes on tasks, checks immutable active authority/context/capability/identity, reserves all resource dimensions, inserts the execution and durable outbox event in one transaction. No network call occurs in admission. Trigger-based failure injection covers reservation, execution and outbox writes; failed transactions leave neither events nor reservations.

Same idempotency key + canonical proposal digest returns the original record, including original expiry/epoch, even after revocation. It never refreshes the grant. A different digest conflicts. A unique in-flight semantic key excludes random idempotency keys and rewritten purpose; the full legitimate-retest policy is ENG-12. Before actual request start, CheckExecutionGrant rechecks current task phase, epoch/version, context and persisted expiry. Only committed outbox rows are visible across connections; consumers may observe duplicates and must claim a lease (ENG-08).

This release only admits registered http.get v1 on logical localhost or loopback IP, within operator-approved target/port and anonymous identity. It reserves a worst-case three HTTP requests, 3*64KiB response bytes and one in-flight slot. Adapter enforcement/pinning is ENG-06. No Shell/browser/MCP/custom executable capability exists in this port.

Tasks receive a separate initial context version; normal graph writes do not bump it. Experiments must belong to the host-resolved exploration. Formal required-claim and plan-dependency adapters are intentionally fail-closed until ENG-10/11: legacy facts and unchecked plan rows cannot authorize actions. They are not silently treated as valid evidence.

The new execution/attempt/outbox tables are additive. Execution identity/proposal/grant columns are immutable. Authority/accounting/history is retained across task deletion; restore/reconciliation integration is ENG-17. The dispatcher remains unmounted until the controlled Worker integration.
