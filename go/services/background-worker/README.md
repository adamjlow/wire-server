# Optional Go background-worker (under construction)

This module is an early runnable slice, not an available service replacement.
Haskell remains the supported default. Both original configuration files and CLI
paths are accepted. Health/readiness and the federation-notification consumer are
implemented with verified RabbitMQ TLS, paginated queue discovery, single-active
quorum queues, raw-body forwarding over internal HTTP2, version selection,
ACK/reject/retry and cancellation without acknowledging unsent messages.

The DLQ watcher now writes the existing Cassandra missed-notification state with
LocalQuorum before ACK; transient mode1 is ignored/ACKed and malformed headers
or write failures remain unacknowledged through channel reconnect.

Rabbit group-sync jobs, PostgreSQL scheduled adminless/meetings
workers and all seven migration families are still being implemented. Those unfinished workers
remain unready; enabled migrations fail startup explicitly. This state
must not be selected for a deployment. Final acceptance remains the whole service
in the frozen reference contract, original six-service tests and real durable
Haskell-Go-Haskell switchback. There is no final compatibility or image claim.

Use pinned Go1.26.9 with `GOTOOLCHAIN=local GOWORK=off`. Build the command at
`./cmd/background-worker`; use `CGO_ENABLED=0` for the Nix Docker harness. Supply
Rabbit credentials using the existing `RABBITMQ_USERNAME` and `RABBITMQ_PASSWORD`
environment variables. Errors and diagnostics do not print credentials or job
payloads. Inherited queue-blockage/drop behaviours are documented in the contract.

The first compiled process (source bdfb252dc3e1a87c14eb282ef6982e3d3194548b)
matched retained actual Haskell `/i/status` 200/empty and exited0 on SIGTERM.
Its unstarted workers correctly reported500. This was a health-only comparison,
not an E2E or Rabbit/storage switchback. Exact static build/compiler/hash and
comparison receipts are in evidence/early-20261009. Required Go checks passed
for the early Go source; final checks must be repeated when the implementation
is complete. Discovery retry budget, graceful in-flight draining and complete
configuration/source-gap coverage still need refinement alongside the remaining
worker families. The existing intermittent peer transport issue stays open.

Rabbit job delivery and group-sync business logic are now implemented as
construction components, with source-shaped envelopes, retries/ACK rules,
metrics, member deltas, actor and recipient propagation. PostgreSQL group queries
and pool/password-file configuration are added. The consumer is not started until
conversation storage and local/remote/bot notification adapters are finished.
Original gundeck40 with the earlier watcher binary ran38/40 pass, cleanup0:
version-header mismatch (corrected) and missing jobs queue (unfinished startup).
See evidence/watcher-20261009 and evidence/group-jobs-20261009 for exact limits;
this is not original worker47, final six-Go124 or durable switchback validation.

PostgreSQL conversation reads/upserts and local/remote join notification adapters
are now implemented. Focused HTTP tests include modern/V0 remote bodies and
empty-recipient Cells pushes; PostgreSQL administrative-shutdown retry matches
the source reset budget. Real bot callbacks and Cassandra/migration routing
remain before consumer startup. See evidence/conversation-adapters-20261009;
these checks do not qualify actual group-job/database E2E or the whole worker.
