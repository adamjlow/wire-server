# Optional Go background-worker (under construction)

This module is under construction, not an available service replacement.
Haskell remains the supported default. Both original configuration files and CLI
paths are accepted. Health/readiness and the federation-notification consumer are
implemented with verified RabbitMQ TLS, paginated queue discovery, single-active
quorum queues, raw-body forwarding over internal HTTP2, version selection,
ACK/reject/retry and cancellation without acknowledging unsent messages.

The DLQ watcher now writes the existing Cassandra missed-notification state with
LocalQuorum before ACK; transient mode1 is ignored/ACKed and malformed headers
or write failures remain unacknowledged through channel reconnect.

Rabbit group-sync consumption is connected with PostgreSQL/Cassandra conversation
storage, migration-aware routing and local, remote and pinned-TLS bot notification
adapters. Six migration families are connected: block lists, password reset,
conversation codes, domain registration, team features and users. User migration
includes SSO/value decoding, related Cassandra reads and claim retention,
Serializable PG copy and pending-delete ledger, sequential CQL cleanup, and the
bounded parallel scan under exclusive UUID locks. Conversation migration remains
unfinished and its flag explicitly rejects startup.

PostgreSQL scheduled adminless/meetings workers remain disconnected. Claim,
heartbeat, retry and acknowledgement storage components exist; schema bootstrap,
scheduled runtime and domain handlers remain unfinished. Actual database effects
for these migration and scheduled components have not been validated. This state
must not be selected for a deployment. Final acceptance remains the whole service
in the [reference contract](../../../docs/go-migration/services/background-worker/contract.md)
and [plan](../../../docs/go-migration/plans/background-worker.md): the final six-Go
mixed-stack original tests, bounded operations and durable Haskell-Go-Haskell
switchback. There is no final compatibility or image claim.

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

The unchanged original gundeck suite passed40/40 with the exact worker executable
from source00557161c5218fba7ce290ba57536561298a35e3 and remaining Haskell peers.
[Its receipts](evidence/jobs-original-20261009/) identify the static binary, strict
XML results and successful owned cleanup. This establishes notification, DLQ and
queue lifecycle behavior for that artifact; it does not validate later migration
construction, the original worker47, final six-Go124 or durable switchback.
Earlier failed38/40 [watcher receipts](evidence/watcher-20261009/) remain historical
evidence for their earlier artifact.

Construction check receipts live under [evidence/](evidence/), including
[user transaction/cleanup](evidence/user-migration-store-20261009/) and
[user value decoding](evidence/user-migration-values-20261009/). These checks do
not establish actual database parity. Current completion status is owned by the
[active task](../../../.migration/tasks/background-worker-implementation.json).
