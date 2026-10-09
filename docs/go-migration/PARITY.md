# Parity evidence and future runner

Reuse `integration/` public-interface tests and existing API serialization goldens
where suitable. Inspect existing service unit suites and operational configs first.
Do not duplicate them blindly or import internal Haskell types into a new oracle.
The proxy has an executable live runner and an automated entry point in
[tools/parity](../../tools/parity/README.md). The general design below governs
other service adapters; it is not evidence that every adapter already exists.

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
gates. Integration execution requires an explicitly authorized environment; this
design alone supplies no deployment or integration permission. The historical
2026-10-07 [preparation plan](plans/delegated-review.md) covered synthetic reference
characterization only. Subsequent user authorization and
[private Docker environment](plans/e2e-environment.md), extended by individual
service plans, permit the selected original suites in owned disposable networks.
This does not authorize shared/production targets or arbitrary integration runs.

Selection evidence must demonstrate omitted selection preserves existing Haskell
defaults; explicit per-service Go selection works with supported peers; invalid or
unavailable selections fail clearly; and switching back to Haskell preserves Go-
written data and queued work. Check existing configuration/image overrides and
ordinary install/upgrade commands. Haskell builds and normal development paths
must remain usable without Go tooling. No Go-only schema or protocol change is
permitted merely to make switching possible.

## Automated acceptance and upstream test reuse

The AI owns executing and reviewing the checks; no manual user testing is an
acceptance dependency. Agree a finite matrix per service before building. Reuse
original `integration/` assertions against Haskell and an image/binary substitution
first. Add differential cases for uncovered security, error, version, storage,
retry or switchback behavior. Further tests require a concrete missing risk,
regression or upstream change. A small load smoke is sufficient unless service
risk establishes a need for a larger benchmark. Runtime GC diagnostic values are
not expected to equal across languages; semantic deviations remain documented
contract decisions, not hidden exclusions of customer API/effects.

The additive e2e adapter stages real service directories and `dist/` links in an
external temporary layout. `Testlib.Env` derives that layout from the selected
`services/integration.yaml`; `Testlib.ModService` and `run-services` then start
selected executables without changing tests. Required case catalogues and XML
checks prevent empty, missing, skipped or failed suites from being green. XML
verification alone is not authenticated execution provenance. Run manifests must
also retain actual binary identities, environment, process exits and source pins.

The [Docker harness](../../tools/parity/docker/README.md) provisions pinned
compiled Haskell tests/peers and disposable Cassandra/RabbitMQ/Redis/S3 and other
fixtures without installing backend packages on the host. The
[current patched qualification](qualifications/go-patched-20261009/README.md)
records77 original tests passing with five Go services and Haskell peers, exact
executable hashes and cleanup0. This is selected-suite evidence, not the entire
backend suite or coverage of the next service's unexamined jobs/migrations.
Preflight/negative runner tests alone still cannot establish e2e success.
Native local GHC/Cabal/Nix checks remain unavailable/unrun; unchanged Haskell code
keeps upstream CI. New service scopes require baseline discovery and an explicitly
recorded finite acceptance matrix in the same authorized disposable environment.
