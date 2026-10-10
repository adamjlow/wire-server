# Optional Go background-worker (under construction)

This module is under construction, not an available service replacement.
Haskell remains the supported default. The exact current six-Go/Haskell-peer
run passes **124/124 original executions**, including all47 worker cases, with
no failures/errors/skips and successful owned cleanup. [Exact receipts](evidence/six-go-original124-20261010/)
identify clean source89f24e7a and every selected executable. Older SCIM failures
remain historical; none was skipped or waived. The unchanged catalogues contain
120 distinct functions because four Events cases overlap across suites.

An optional scratch image now includes the same worker, static patched OpenSSL3.5.9,
reference CA bundle and license notices. Existing bot/DHE/pin/date/cancellation
tests pass in that exact image. [Construction receipts](evidence/image-construction-20261010/)
do not qualify real image health/default mounts, loaded operations or actual
same-state Haskell-Go-Haskell switchback. These, the separately scoped selector,
and final review remain pending; compatibility remains null.

Both original configuration files and CLI
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
is complete. Later construction implements the native discovery retry budget
and scheduled per-pool drain. Stored error diagnostics and connection/header timeout phases now have
[construction receipts](evidence/http-diagnostics-20261010/). The ordinary HTTP
proxy/reused-connection audit and final process validation remain outstanding.
A separately reviewed fixture fix supports the measured1702-FD runner demand;
the remaining SCIM failure is explicit in the current checkpoint above.

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
storage construction. The reaper is now connected to the scheduled runtime;
this earlier receipt does not qualify the whole service.

Cron storage passed an isolated PostgreSQL check for operator override retention,
queue healing, manual-request expiry/claim rollback, monotone watermarks and
transaction-scoped leadership. [Exact receipts](evidence/scheduled-cron-storage-20261009/)
cover that earlier storage checkpoint. Cron parsing, insertion and scheduler
construction now also pass [Go and isolated PostgreSQL checks](evidence/scheduled-cron-runtime-20261009/),
including native job fields, overlap/manual rules, DST dedup, insertion failures,
startup/wake and cancellation. Cron is now connected to the service runtime;
this earlier receipt does not qualify live domain adapters or complete startup.

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
removal adapters and service wiring are now connected. This earlier storage
fixture explicitly stubs missing conversations. At that checkpoint the full meetings handler and original cleanup test were not
qualified; the current original47 run above now passes the native cleanup case.

Required meetings configuration and one-run retention cutoff now pass
[Go construction checks](evidence/meetings-config-20261009/). Source cron
serialization and fixed-point/SQL timestamp arithmetic are preserved, including
large finite timestamp wrap as an inherited concern. Concrete deletion and runtime/counter integration are connected; complete mixed-stack
qualification remains unfinished. This construction evidence is not original-suite proof.

The source bot finite-DHE suites now use a restricted authenticated OpenSSL stream
fallback when Go receives the remote TLS handshake-failure alert. Mandatory pin,
signature and date checks run before any HTTP data; authentication failures never
fall back. [Construction receipts](evidence/bot-dhe-20261010/) include full Go checks
and a real finite-DHE fixture with patched OpenSSL3.5.9 built in Docker. The optional
image must supply a supported patched `openssl` on its trusted PATH and retain the
Apache2.0 license; absent CLI fails the callback. Final image/mixed-stack/lifecycle
qualification is still outstanding. No backend credentials are passed to the helper.

[Ordinary HTTP construction receipts](evidence/http-proxy-replay-20261010/)
record Haskell-compatible proxy snapshots and recovery from incomplete headers
on reused connections. Fresh, one-shot chunked, invalid-status, timeout and
cancellation behavior remain guarded. Full pinned Go checks pass. This does not
qualify later executable/image or final mixed-stack/operations/switchback work;
remaining ordinary-parser audit is explicit in the plan and task handoff.

Build the optional helper entirely in Docker with
`python3 go/services/background-worker/build_openssl.py --output /absolute/new/cache-directory`
(optional `--archive` uses the publisher-checksummed cached source). It installs
compiler packages only in its disposable builder and retains actual package/source
identities and license notices. Package identified artifacts using
`build_image.py --output /absolute/new/receipt.json --candidate WORKER --candidate-receipt BUILD.json --helper OPENSSL --helper-receipt HELPER-build.json`
with the pinned Go1.26.9 on PATH and a clean checkout. Both commands are opt-in;
they never publish, deploy or change Haskell tooling/defaults. Image startup uses
the existing two config mounts and Rabbit credential variables.
