# ADR-008: fencing, cancellation and unknown effects

Execution grant expiry and worker lease expiry are distinct (five-second request-start grant, thirty-second lease). Every actual Worker adapter request uses a policy bound to its host-issued attempt ID/owner/epoch; stale, cancelled or expired attempts cannot start new requests or commit new results. Already stored idempotent results are readable without renewing authority.

Get/Wait read the same execution. Wait has a bounded deadline and no submission port. Cancellation is first persisted. Only an actual local supervisor stop acknowledgement can confirm local connection cleanup; no active local owner means unconfirmed. Queued-never-started cancellation is provably effect-free and can settle zero; unknown running/lost costs remain reserved. A local close does not establish remote cleanup.

A bounded scanner selects up to128 expired attempts, then locks task -> execution/attempt, increments fencing epoch, records lost/unknown/unconfirmed and durable events. It never refunds reservations or queues a new attempt. Runtime scanning is tied to the cancellable server lifecycle; no assistant background runner is implied. Server restart suspends prior controlled tasks in draining with a new epoch, never applies the legacy intent-reset/auto-resume path. Operator reconciliation or an explicitly reauthorized restored task is required.

Unknown lost effects stop ordinary new admissions for that task; host-only reconciliation may use protected reserves. Semantic replay cannot be unlocked by a nonce, rewritten purpose or nil-vs-anonymous identity alias. Registered read methods also report effect=unknown: HTTP success is not proof of a remote state transition or absence thereof.

The synthetic counter protocol is deliberately narrow: the original /increment effect is followed only by a newly authorized read of /effects/<original execution ID>, using the controlled adapter, separate budget and a bound JSON effect receipt. Recovery does not replay /increment. It may record known_applied/known_not_applied while preserving unconfirmed cleanup and unknown original reservations. Arbitrary nonqueryable effects remain unknown/manual. This is not external exactly-once or proof about an unknown remote system.

Tests advance the scanner's host clock instead of sleeping thirty seconds, assert old epoch rejection for result and new calls, compare accounts across recovery/reopen, observe one physical counter increment after dropped report, and retain unknown cancellation without fabricating an acknowledgement.
