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
adapters. All seven migration families are connected: block lists, password reset,
conversation codes, domain registration, team features, users and conversations. User migration
includes SSO/value decoding, related Cassandra reads and claim retention,
Serializable PG copy and pending-delete ledger, sequential CQL cleanup, and the
bounded parallel scan under exclusive UUID locks. Conversation migration runs
native independent local-conversation and remote-membership scans, with complete
member status/subconversation/MLS-history copy and source cleanup/ledger ordering.

PostgreSQL scheduled adminless/meetings workers are connected to the source-based
claim/lease/completion, registry, shared listener, cron/reaper and domain adapters.
Startup applies the native scheduled schema before exposing health; each pool
drains under the configured grace policy and leaves unfinished leases durable.
The native meetings cleanup run counter is exposed. Construction Go checks and
[combined PostgreSQL runtime checks](evidence/scheduled-runtime-20261010/) pass.
These checks exercise PostgreSQL-only routing, guarded adminless jobs and meetings
cleanup; they do not qualify live Cassandra/Brig/Rabbit/bot interactions, the complete
process startup path, all enabled migrations or whole-service lifecycle. This state
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

Native scheduled schema setup passed an isolated Docker PostgreSQL check:
227 original Haskell column records match exactly;76 migration records, queued-job
preservation on rerun and checksum rejection pass. [Its receipts](evidence/scheduled-schema-20261009/)
cover this limited storage construction, not full scheduled execution or switchback.

The five scheduled reaper operations passed an isolated Docker PostgreSQL check,
including lease guards, tree-DLQ snapshots/parent resume, shared gate exclusion and
timeout rollback. [Exact receipts](evidence/scheduled-reaper-20261009/) cover this
storage construction. The reaper is not started until scheduled runtime exists.

Cron storage passed an isolated PostgreSQL check for operator override retention,
queue healing, manual-request expiry/claim rollback, monotone watermarks and
transaction-scoped leadership. [Exact receipts](evidence/scheduled-cron-storage-20261009/)
cover that earlier storage checkpoint. Cron parsing, insertion and scheduler
construction now also pass [Go and isolated PostgreSQL checks](evidence/scheduled-cron-runtime-20261009/),
including native job fields, overlap/manual rules, DST dedup, insertion failures,
startup/wake and cancellation. Domain handlers and runtime integration remain
unfinished; cron is not started by the service.

The unchanged original Haskell MLS migration test passed1/1 on source
309733bb48febdb8708e6b1c5aadb94cb03fe8ab with only Go worker/Haskell peers.
[Exact receipts](evidence/conversation-original-20261009/) cover all five storage
phases, enabled local/remote copying/cleanup, PostgreSQL-only behavior and final
shared MLS history, with strict XML and owned cleanup. The initial counter-text
failure remains retained; the candidate was fixed without changing assertions.
This is limited conversation proof, not full worker or six-Go qualification.
The users2 run still fails before migration activation on SCIM500; it does not
validate enabled user migration and must be resolved before final acceptance.

Construction check receipts live under [evidence/](evidence/), including
[user transaction/cleanup](evidence/user-migration-store-20261009/) and
[user value decoding](evidence/user-migration-values-20261009/). These checks do
not establish actual database parity. Current completion status is owned by the
[active task](../../../.migration/tasks/background-worker-implementation.json).

Meetings storage and cleanup orchestration now pass [construction checks](evidence/meetings-cleanup-storage-20261009/),
including actual PostgreSQL execution with9 readonly original migrations. Native
expiry ordering/limits, open/boundary retention, full-row validation, code routing,
batch/error behavior and deletion guards are implemented. Conversation/proposal
removal adapters and service wiring remain
unfinished; this fixture explicitly stubs missing conversations. No full meetings
handler or original cleanup-test qualification is claimed.

Required meetings configuration and one-run retention cutoff now pass
[Go construction checks](evidence/meetings-config-20261009/). Source cron
serialization and fixed-point/SQL timestamp arithmetic are preserved, including
large finite timestamp wrap as an inherited concern. Concrete deletion and runtime/counter integration are connected; complete mixed-stack
qualification remains unfinished. This construction evidence is not original-suite proof.
