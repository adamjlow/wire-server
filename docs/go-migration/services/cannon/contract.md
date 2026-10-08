# Cannon observable contract

Reference source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; qualified publisher
runtime and binary identities are in [oracle/runner-preflight.json](oracle/runner-preflight.json).
This is the whole-service target for optional Go cannon, including both classic
notifications and consumable events. Haskell remains supported and the default.
The [inventory](inventory.md) maps entry points; the [plan](../../plans/cannon.md)
fixes the finite acceptance matrix. A source-reviewed expectation is not a passed
runtime assertion. Required baseline and candidate reports must actually pass.

## HTTP and classic sockets

[Public routes](../../../../libs/wire-api/src/Wire/API/Routes/Public/Cannon.hs)
and [public handlers](../../../../services/cannon/src/Cannon/API/Public.hs)
expose `/await` and its `/websocket` alias. Both require ingress `Z-User` UUID
and `Z-Connection` opaque connection ID, and accept an optional hexadecimal
`client`. Preserve the existing ingress authentication boundary: cannon trusts
these headers and does not introduce a bearer-token check or Brig ownership RPC.
Shared [version middleware](../../../../libs/wire-api/src/Wire/API/Routes/Version/Wai.hs)
handles the configured disabled versions; events are unavailable before v8,
v8 ignores `sync_marker`, v9+ accepts its text value. Missing/malformed typed
inputs are rejected before presence/queue effects.

[Actual ten-request capture](oracle/http-reference.json) freezes status, plain
body, Content-Type and Allow for health, missing/invalid principal, invalid client,
invalid internal UUID, malformed bulk JSON, pre-v8 events and a valid non-upgrade
await. In particular, non-upgrade is 426 with an empty body; Servant validation
errors have no Content-Type and are not replaced by a generic JSON error.
Date, trace identifiers and request IDs may vary; response status and semantic
error body may not. Keep generic provider exceptions from exposing credentials.

[App.hs](../../../../services/cannon/src/Cannon/App.hs) accepts classic WebSockets,
registers a local generation, then registers remote gundeck presence. Registration
failure removes the matching generation and closes the accepted socket. Presence
POST `/i/presences` carries `user_id`, `device_id`, `resource` and `client_id`
(including null when absent); resource is the advertised external host and listener
port followed by `/i/push/:user/:conn`. [WS.hs](../../../../services/cannon/src/Cannon/WS.hs)
uses four total attempts with three exponential 100-ms retries for eligible RPC
failures. Preserve the source's exception eligibility rather than retrying every
HTTP rejection indiscriminately. HEAD internal presence checks GET gundeck's
`/i/presences/:user`; remote registration is authoritative, not local dictionary
membership. No new presence store or schema is introduced.

Classic notifications are binary WebSocket frames. Single push sends the exact
raw request bytes, including JSON whitespace; it does not re-encode. Text or binary
application `ping` returns binary `pong`. Control frames use WebSocket ping/pong
handling. The source library [documents its defaults](https://hackage-content.haskell.org/package/websockets-0.13.0.0/docs/Network-WebSockets-Connection-PingPong.html)
as ping every 15 seconds and pong timeout 30 seconds; preserve these classic
defaults independently of the event wsOpts. Maximum classic lifetime is three days. Local keys are `(user,connection)`;
a replacement generation supersedes its predecessor. Closing the old generation
must not delete or close the newer socket. Writes, including pongs, are serialized
without reordering notification delivery; failed sends retry as in the reference
and eventually remove only the matching generation.

[Internal routes](../../../../libs/wire-api/src/Wire/API/Routes/Internal/Cannon.hs)
and [handlers](../../../../services/cannon/src/Cannon/API/Internal.hs) define:

| Request | Response/effect |
| --- | --- |
| GET `/i/status` | 200, empty |
| POST `/i/push/:user/:conn`, JSON raw body | binary forward and 200 empty; absent/failed socket 410 `{code:410,label:"general",message:"client gone"}` |
| POST `/i/bulkpush` | typed notification per target, ordered `bulkpush_resp` results |
| HEAD `/i/presences/:user/:conn` | remote registered 200; absent 404 (`not-found`, `presence not registered`), HEAD body empty |

[Bulk schema](../../../../libs/wire-api/src/Wire/API/Internal/BulkPush.hs):
`bulkpush_req` is an array of `{notification,targets}`; notification requires UUID
`id`, nonempty array of object `payload`, optional boolean `transient` default
false. Targets require UUID `user_id` and text `conn_id`. The typed notification
sent on a classic bulk socket includes the transient field. Results preserve
notification/target input ordering, each `{notif_id,target,status}` with
`push_status_ok` or `push_status_gone`. Concurrent target work does not permit
response permutation. Extra JSON fields follow upstream schema acceptance; strict
required types/nulls/nonempty payloads must not become Go zero-value acceptance.

## Consumable event sockets, queues and recovery

[RabbitMqConsumerApp.hs](../../../../services/cannon/src/Cannon/RabbitMqConsumerApp.hs)
creates a RabbitMQ channel and queue before accepting `/events`; exhausted pool
capacity rejects the handshake 503 empty. One channel per event socket, prefetch
500 and explicit acknowledgement, permits event delivery without waiting for each
ack. Channel failure sends close 1001 with empty reason. Broker connection recovery
creates capacity for new sockets; an existing socket is not silently rebound.

[Protocol](../../../../libs/wire-api/src/Wire/API/Event/WebSocketProtocol.hs):

| Direction | Exact JSON shape, binary server frames |
| --- | --- |
| server event | `{type:"event",data:{event:{id,payload},delivery_tag}}` |
| server missed events | `{type:"notifications_missed"}` |
| server sync | `{type:"synchronization",data:{marker_id,delivery_tag}}` |
| client ack | `{type:"ack",data:{delivery_tag,multiple}}` |
| client full sync ack | `{type:"ack_full_sync"}` |

Queued event schema includes UUID id and nonempty object payload; unlike classic
Notification it does not add transient. Ack delivery_tag is an unsigned 64-bit
integer, and multiple is a required boolean. Missing/null/wrong-type fields,
fractions, negatives and overflow cannot decode as a valid zero ack. Full-sync
ack outside full-sync state, or ordinary ack while waiting for it, closes 1003
`unexpected-ack`. Malformed client JSON/protocol closes 1003 `failed-to-parse`.
Malformed queued JSON is rejected without requeue and consumption continues.
A transport/protocol failure closes 1003 `websocket-failure`, generic exceptions
1003 `internal-error`; do not put parser/provider secrets in close reasons.

Default event activity and pong intervals are 20,000,000 microseconds. Control
ping updates activity and elicits pong; pong wakes heartbeat. Data acknowledgements
do not reset activity in this reference. When inactive, send control ping, wait
for pong and close 1002 `inactivity` on expiry. Normal final teardown attempts an
empty normal close and ignores a failed closing write.

[RabbitMq.hs](../../../../services/cannon/src/Cannon/RabbitMq.hs) starts one pooled
connection and names connections `pool N` (observable in original broker-admin
assertions). Positive connection/channel limits default to 1000/300. Choose a
least-used active connection, open another when appropriate, and bound capacity
race retries to ten 10-ms retries. Draining rejects new channel acquisition.
Do not infer exclusive-consumer behavior from the name of testSingleConsumer:
source consumes non-exclusively; queue declarations do not enable single-active
consumer. Existing queued events are prefetched by the first consumer.

[Queue contract](../../../../libs/wire-api/src/Wire/API/Notification.hs): exchange
`user-notifications`; client queue `user-notifications.UUID.clientId`; routing
keys `UUID`, `UUID.clientId`, `UUID.temporary`. Client queues retain quorum type,
existing declaration flags and dead-letter exchange/key `dead-user-notifications`.
Temporary queues are server-named, exclusive and auto-delete, bound to user and
temporary keys. Actual temporary queue metadata is non-durable, exclusive, auto-delete and
classic type (broker-injected x-queue-type); capture selects by flags rather than
a guessed generated-name prefix. Closing channel/connection removes temporary queues. Client queues
survive process replacement; unacknowledged messages redeliver on reconnect. No
new queue arguments, exchange layout, subscription store or storage migration.
Read-only [actual Haskell-created queue flags](oracle/queue-flags-reference.json)
confirm durable=true, auto_delete=false and exclusive=false with exactly those
three quorum/dead-letter arguments; candidate declarations must match.

For a configured client, [full-sync queries](../../../../services/cannon/src/Cannon/RabbitMqConsumerApp.hs)
read existing gundeck `missed_notifications(user_id uuid,client_id text)` using
prepared, parameterized LocalQuorum SELECT with source retries. If a row exists,
send notifications_missed and wait for ack_full_sync before delivery proceeds.
Only that ack deletes the row with prepared LocalQuorum DELETE; an invalid or
aborted handshake must not erase missed state. The table is defined in existing
[gundeck V12](../../../../services/gundeck/src/Gundeck/Schema/V12.hs). Haskell↔Go
switchback must recover both unacked broker delivery and missed-row state.

For client+sync_marker, publish to the queue via default exchange with raw UTF-8
marker body, type `synchronization`, persistent mode, content type
`text/plain; charset=utf-8` and expiration equal to the decimal notificationTTL
configuration value. Preserve the raw expiration rather than guessing a unit
conversion. FIFO marker ordering and multiple ack are required. Expired and
transient events keep the original dead-letter/full-sync behavior supplied by
Haskell producer and background worker.

## Configuration, operations and security disposition

[Options](../../../../services/cannon/src/Cannon/Options.hs) and
[Run](../../../../services/cannon/src/Cannon/Run.hs) are the configuration reference:
`-c` with default `/etc/wire/cannon/conf/cannon.yaml`; JSON/YAML same field names;
listener/externalHost/externalHostFile, gundeck, rabbitmq, cassandra, drainOpts,
notificationTTL, disabledAPIVersions and log options. Literal externalHost takes
precedence; externalHostFile is trimmed; missing both fails. Unknown shared fields
remain tolerated. Missing required values fail; reject zero grace/batch interval,
nonpositive broker limits and invalid typed ports/versions. Preserve optional
wsOpts defaults. RabbitMQ credentials come from required environment variables,
not compiled-in credentials. Cassandra endpoint/keyspace, tlsCa and optional
datacentre filter remain usable; no host runtime/backend installation required.

TLS is verified against system/custom CA; the fixture uses actual repository
synthetic CAs. No test uses skip-verification. Existing AmqpEndpoint exposes an
explicit insecureSkipVerifyTls option; its security disposition remains visible
below, never enabled to make validation pass. SQL/CQL is parameterized. Gorilla's
implicit cross-origin restriction must not silently change the upstream handshake;
characterize source origin acceptance and mirror the established ingress boundary.
Strict JSON/UUID/client validation and serialized writes protect the migrated
boundary without introducing a new authentication model or storage design.

SIGINT/SIGTERM catches once, drains classic connections then broker channels in
configured batches and exits 0. Classic drain close is 1000 `close`; channel drain
propagates event 1001 empty. The finite bound is twice fixture grace plus two
seconds, reflecting two sequential phases. Health, Request-Id behavior, generic
errors, gzip, structured logging and normalized HTTP metrics remain available.
`net_websocket_clients` is a gauge with HELP Number of connected websocket clients,
refreshed each second for classic sockets only. Runtime-specific GC/process
metrics need not numerically equal Haskell; operationally useful metric shapes do.
One bounded 40-request/socket smoke at concurrency two measures RSS <=192 MiB
and p95 <=2*Haskell+100 ms. Do not turn it into an open-ended benchmark campaign.

Serious inherited concerns are recorded separately, without a bundled behavior fix:

- Source debug event logs include the full notification payload; classic trace
  logs include 129 bytes. Sensitive notification contents can enter logs when
  those levels are enabled. Proposed separate improvement: metadata-only logging
  with explicit redaction and a reviewed logging contract.
- Explicit insecureSkipVerifyTls in shared AMQP options bypasses certificate
  validation; secure default is false. Proposed separate improvement: remove or
  strictly constrain that option after operator/contract review. No bypass is
  approved or needed in this migration's acceptance environment.
- Drain configuration interval greater than grace produces a zero divisor in
  batch computation during shutdown. Proposed separate improvement: startup
  validation with an explicitly reviewed invalid-config response. Valid fixture
  configuration is not affected; do not silently claim invalid-config identity.
- Synchronization expiration uses raw notificationTTL while AMQP expiration is
  milliseconds. Preserve source behavior; any TTL-unit correction is separate
  and must account for current operators and queue effects.

These are source-backed inherited concerns, not claims of a demonstrated attack
against an actual deployment. Production network/TLS/auth changes require their
own authorization; optional Go availability changes no live deployment.

## Finite validation and equality rules

The [exact original catalogue](../../../../tools/parity/e2e-tests.json) has 35 cannon
compiled cases: Events 26 including both federated typing variants, Notifications
seven, Demo two. Execute unchanged original assertions on both Haskell and Go in
the same private fixture family. Required names/counts, no skips, nonzero exits,
missing reports and failure/error nodes are all fail-closed. Original cases already
cover most events/isolation/prefetch/sync/reconnect/queue cleanup/failure behavior;
do not duplicate their entire protocol suite.

Supplement only these concrete gaps: ten HTTP malformed/non-upgrade observations;
classic alias/raw single push and ordered mixed bulk statuses, app/control pong,
duplicate-generation cleanup, remote presence and registration failure; malformed
ack close behavior; config/CLI/security defaults, metrics, bounded operational
smoke and loaded shutdown; cross-runtime unacked and missed-state switchback.
Freeze stimuli/source expectations before candidate use and retain actual
reference outcomes. Queue flags, origin policy and parser details that need actual
reference characterization remain explicit qualification work, never guessed
passes. [Failed baseline 1](oracle/baseline-1-review.md) is retained with all five
failures; idle forwarding is repaired and [the same 35-case rerun passed](oracle/baseline.run.json), with
all-Haskell executable identities and successful owned cleanup retained.

Compare exact statuses/headers, semantic JSON object keys/typed values, frame
opcode/payload/close code/reason, ordered results and provider/storage effects.
Raw single-push bytes compare exactly. Preserve missing versus null fields and
integer precision. Normalize only validated generated IDs, declared clock bounds,
source-valid RabbitMQ delivery-tag mapping across separate runs and incidental
runtime diagnostics; never erase isolation, status, payload, ordering or effects.
Fixture/source/compiler/image/binary identities belong in every qualifying receipt.
Final combined acceptance runs original 5 proxy+27 cargohold+35 cannon cases with
all three selected Go binaries and remaining Haskell peers, followed by scoped
same-author review delegated explicitly by the user. Neither discovery, this
contract nor dependency proof claims candidate parity or availability.

Reference supplements now retain [28 boundary observations](oracle/boundaries-reference.json), including cross-origin acceptance, four failed-registration attempts, all three malformed-ack close reasons, event inactivity, classic gauge and both loaded drain close frames. The raw 40-request p95/RSS and successful 1.02-second exit are reference measurements, not candidate proof. [Six startup rejections and CLI help](oracle/config-reference.json) retain actual exit codes; runtime diagnostic prefixes/backtraces and temporary file names are not API identity targets. All stimulus scripts remain protected oracle paths.

The existing reviewed inventory task explicitly includes the finite executable plan and source/default-preservation checks. Reuse that done discovery task for the plan milestone; do not create a duplicate discovery just to restate the same plan. The pinned Go dependency proof supplements that plan with compiled, license-checked and vulnerability-scanned module versions before implementation. This reuse does not convert plan/reference work into candidate evidence.

Capture helper correction before Go consumption: the actual supplemental run used a fixed 17-byte handshake challenge (accepted by Haskell); the canonical candidate helper uses the intended RFC6455 16-byte challenge. The exact executed script is retained separately and hashed in its receipt. Original reference Demo/Notifications tests cover standard challenge acceptance. Challenge values and dependent Sec-WebSocket-Accept are incidental session values, not equality targets. No malformed-key equivalence or approved protocol-security exception is inferred.

Reviewed baseline: original 35/35 pass, no failures/skips; process exit0 and cleanup exit0 at controller 0575d90e83caa1cfcb80774ae8b83f7d3b152de6, runner sha256:f88496cd7850c4a32563e2085d41d2647e1038682a61fe0ffb5540c07f88ad7b. Both previously failing PostgreSQL-backed notification cases and both typing variants pass. Idle forwarding regression passes, unchanged source assertions retained. Focused checks: eight harness tests, fourteen container/controller tests, thirty-two state tests, scoped check and diff check pass. No native Haskell/GHC/Nix tools available; no Haskell source or dependencies changed and existing upstream CI remains required.

Review: codex (self-review delegated by user), same actor as author. Authority is the user’s explicit request to review and self-approve, subsequent autonomous-development instruction, complete-cannon goal and mirror-current-backend/security-notes disposition. This approves the finite source contract, actual reference qualification and reusable reviewed plan/dependency choice only. It approves no insecure TLS use, contract exception, candidate parity, default change, production switch or remote push. Implementation may now consume this frozen oracle.
