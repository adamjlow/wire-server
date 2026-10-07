# Parity evidence and future runner

Reuse `integration/` public-interface tests and existing API serialization goldens
where suitable. Inspect existing service unit suites and operational configs first.
Do not duplicate them blindly or import internal Haskell types into a new oracle.
There is no differential runner in this patch; the following is its design contract.

## Run interface

A future runner accepts a versioned manifest containing: service; imported upstream
commit; immutable reference source/image digest; candidate source/image digest;
contract/fixture revision; scenario IDs; API versions; config digests; deterministic seed/clock;
isolated reference and candidate endpoints; dependency image versions; timeout;
normalization decision IDs; and artifact destination. Validate an allowlist of
test endpoints and disallow production targets by default. Never take a shell
command from a task record. Provisioning credentials stay outside artifacts.

For each scenario, reset two isolated, equivalently seeded environments. Run the
same stimulus against reference and candidate, then compare responses and observed
effects. Stub third-party delivery/payment/email/push/storage services as needed.
Do not send duplicate live writes or make two consumers race on a production queue.
Peer-service matrices must include Go candidates interacting with Haskell peers.
Keep an all-Haskell baseline and test each advertised mixed configuration. Preserve
existing Haskell checks; Go-only success never substitutes for them.

Compare status, semantic headers, byte/JSON body semantics (missing versus null,
numeric representation, ordering where contractual), errors, authentication and
authorization, API/federation versions, WebSocket ordering/reconnects, queue
delivery/ack/retry/idempotency, storage effects and consistency, timeouts and
partial failures. Include malformed inputs, resource limits and negative cases.

Normalize only documented nondeterminism: for example, map generated identifiers
consistently across an entire scenario and compare timestamp constraints, not
blindly delete fields. Assert preconditions before normalization. Every rule needs
a separate reviewed contract decision and a test showing a meaningful mismatch
still fails. Never sort ordered events or ignore errors just to obtain a pass.

## Result interface

The future runner emits versioned JSON with `run_id`, all manifest revisions and
digests, environment identity, start/end times, per-scenario result
(`pass|fail|blocked|skipped`), comparator/normalization version, sanitized mismatch
artifact paths and coverage totals. Exit 0 only if all required scenarios ran and
passed; fail, timeout, missing scenario or blocked environment exits nonzero.
Keep raw sanitized observations alongside comparison output. CI artifacts should
be archived durably for gate evidence, not temporary expiring links alone.

Link that report through the task's `evidence` entries using the matching
criterion ID, exact command, tested revision and artifact URL/path. A sample report
or self-reported success is not passing evidence. Reviewers verify both reference
and candidate hashes from the report; the task revision identifies the candidate
checkout. Record config/fixture changes and rerun all affected scenarios.
Publish exact supported tuples through the service's `compatibility` record, not
a blanket claim of support for the latest upstream. Use the
[upstream-sync procedure](UPSTREAM.md) to invalidate and renew evidence.

## Operational gate

Parity also needs agreed service-specific startup/readiness, metrics/log field,
signal/shutdown, cancellation, resource, load, latency/error-budget and rollback
checks. Set thresholds in the contract before measuring. Document how to switch
back without incompatible persisted data. HTTP parity alone cannot satisfy these
gates. Real integration execution remains blocked by root policy until explicitly
authorized; this design supplies no deployment or integration-test permission.

Selection evidence must demonstrate omitted selection preserves existing Haskell
defaults; explicit per-service Go selection works with supported peers; invalid or
unavailable selections fail clearly; and switching back to Haskell preserves Go-
written data and queued work. Check existing configuration/image overrides and
ordinary install/upgrade commands. Haskell builds and normal development paths
must remain usable without Go tooling. No Go-only schema or protocol change is
permitted merely to make switching possible.
