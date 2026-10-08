# Cannon inventory

Reference/upstream: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Whole-service
replacement includes both classic push WebSockets and RabbitMQ consumable events.
Haskell remains supported/default. The [finite plan](../../plans/cannon.md) governs
implementation and the user's delegated review/current-behavior disposition.

## Runtime and boundary map

| Surface | Primary source and observable behavior |
| --- | --- |
| Process/config | services/cannon/exec/Main.hs; Options.hs, Run.hs: listener/externalHost precedence, gundeck, verified RabbitMQ TLS, Cassandra keyspace, disabled versions, logging, drain/pool limits, notificationTTL |
| Classic public | libs/wire-api/src/Wire/API/Routes/Public/Cannon.hs; API/Public.hs and App.hs: /await and /websocket, ingress Z-User/Z-Connection and optional client, accepted socket registered locally then remotely at gundeck, binary notifications, text/binary ping -> binary pong, 3-day max lifetime |
| Internal push/presence | API/Internal.hs and wire-api Routes/Internal/Cannon.hs: GET /i/status 200; POST /i/push/:user/:conn sends raw JSON bytes as binary or 410 general/client gone; POST /i/bulkpush emits notification/target statuses in input order; HEAD /i/presences/:user/:conn queries remote gundeck presence, 200 or 404 |
| Presence and replacement | WS.hs: POST /i/presences JSON user_id/device_id/resource/client_id, GET /i/presences/:user, retry3x; duplicate key replaces local socket generation; old socket termination removes only matching generation, preserving replacement |
| Event public | /events from API v8; v9 adds sync_marker. Z-User and optional client; RabbitMQ channel/queue acquired before WebSocket accept; resource exhaustion rejects handshake 503 |
| Queue representation | libs/wire-api/src/Wire/API/Notification.hs: user-notifications exchange, user/client routing keys, client queue user-notifications.UUID.clientId, quorum queue arguments and dead-letter exchange/routing-key dead-user-notifications; temporary exclusive auto-delete queue bound to user and user.temporary keys |
| AMQP pool | RabbitMq.hs: startup connection, connection names pool N used by original tests, configurable positive connection/channel caps; QoS prefetch 500 per channel, explicit acknowledgements, bounded retry-on-capacity-race, channel-close propagation, teardown and pool drain |
| Event frames | RabbitMqConsumerApp.hs; wire-api Event/WebSocketProtocol.hs: binary event/data/event/delivery_tag, synchronization marker, notifications_missed; client ack requires delivery_tag/multiple, ack_full_sync is distinct. Concurrent send and ack loops preserve queue delivery order; invalid JSON ->1003 failed-to-parse, unexpected full-sync ack ->1003 unexpected-ack |
| Recovery and effects | Cassandra LocalQuorum prepared SELECT missed_notifications(user_id,client_id); full-sync message waits for ack_full_sync then prepared DELETE. Unacked AMQP redelivers on reconnect, invalid queued JSON rejected without requeue, temporary queues removed on teardown; synchronization marker persisted with configured expiration |
| Timeouts and closure | Activity/pong timeouts default20,000,000 microseconds; event inactivity sends control ping and closes1002 inactivity on timeout. Broker channel close sends1001 empty reason; generic event failure1003 internal-error. Classic control ping/pong uses reference WebSockets defaults |
| Operations | Run.hs signal handler drains classic sockets then AMQP channels in separate configured batches and exits0. Gauge net_websocket_clients counts classic sockets each second; shared HTTP middleware metrics, version filtering, Request-Id, gzip, generic errors and log formats |

Cassandra is not a new cannon-owned schema. The existing gundeck keyspace/table
and RabbitMQ representations must remain compatible across Haskell/Go. Reuse
existing synthetic fixture initialization and peer supervision; do not add host
backend dependencies or rewrite upstream Haskell tests. Standard C/HTTP nginx
and schema tools stay from the qualified publisher image.

## Original tests and actual compiled discovery

Source Events has 25 test functions; Notifications seven; Demo contributes
testModifiedCannon and testWebSockets. Read-only --list inside the pinned runner
confirms all 34 source functions and **35 compiled cases**, including both
federated=true/false typing-indicator variants. No backend/test was executed.
Broad list filtering also returned unrelated MLS/feature tests; these are excluded
from this finite catalogue, not treated as cannon passing coverage.

Runner image: sha256:b6efb1a0a560e6f2ad798efd2d36939eabe82153057742786326fecd630d1f9d,
qualified source a998e043ad4ac5e382ba3609f4c8a332e4fdd8be. Integration executable
SHA256 56f56a8e0f8ffd24a11982f13385d41f4d92e238e6900bcbca1ddf7d21825f4a.
Publisher base: quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27.
Discovery used a non-root, read-only, no-network owned container and --list with
Events/Notifications/Demo filters, removed after completion. Provenance is the
qualified trusted publisher runtime, not independent rebuild attestation.

Required compiled cases (retain exact parameter variants for XML validation):

```text
Demo.testModifiedCannon
Demo.testWebSockets
Events.testBackendPusherRecoversFromQueueDeletion
Events.testChannelKilled
Events.testChannelLimit
Events.testConsumeEventsAckNewEventWithoutAckingOldOne
Events.testConsumeEventsAcks
Events.testConsumeEventsForDifferentUsers
Events.testConsumeEventsMultipleAcks
Events.testConsumeEventsOneWebSocket
Events.testConsumeEventsWhileHavingLegacyClients
Events.testConsumeTempEvents
Events.testEndOfInitialSync
Events.testEndOfInitialSyncAckMultiple
Events.testEndOfInitialSyncIgnoreExpired
Events.testEndOfInitialSyncMoreEventsAfterSyncMessage
Events.testEventsDeadLettered
Events.testEventsDeadLetteredWithReconnect
Events.testEventsForSpecificClients
Events.testPrefetchCount
Events.testSendMessageNoReturnToSenderWithConsumableNotificationsProteus
Events.testSingleConsumer
Events.testTemporaryQueuesAreDeletedAfterUse
Events.testTransientEvents
Events.testTransientEventsDoNotTriggerDeadLetters
Events.testTypingIndicatorIsNotSentToOwnClient[federated=false]
Events.testTypingIndicatorIsNotSentToOwnClient[federated=true]
Events.testWebSocketTimeout
Notifications.testAddClientNotification
Notifications.testBulkPushSameMessage
Notifications.testFetchAllNotifications
Notifications.testGetServerTime
Notifications.testInvalidNotification
Notifications.testLastNotification
Notifications.testTargetClientPush
```

These original tests already exercise acknowledgements, prefetch, reconnect,
dead-letter/full-sync, expired/transient events, queue/channel limits and kills,
client/user isolation, temporary queue removal, legacy interoperability, sync
marker ordering, typing, backend-pusher queue recovery and notification delivery.
Their actual all-Haskell baseline must run before candidate qualification.

## Concrete unknowns and security disposition

- Qualify this catalogue in the private Docker environment, including dynamic
  backends, RabbitMQ administration and both federated typing variants. Discovery
  proves available assertions, not that fixture setup or tests pass. Missing
  infrastructure is repaired in the harness rather than removing required cases.
- Capture exact malformed header/client/handshake/internal JSON responses and
  classic replacement/presence failure behavior where original tests omit them.
  Freeze supplemental assertions separately before implementation consumption.
- Source gives two sequential drain phases. The plan's shutdown bound is therefore
  twice configured grace plus two seconds, fixed before runtime comparison.
  Config with batch interval greater than grace yields a potential division-by-zero
  on shutdown; characterize only if needed for supported config, and document a
  separate improvement rather than silently changing the reference.
- Verify AMQP declaration flags/defaults, close/ack edge semantics and sync-marker
  expiration units against actual runtime before storage compatibility claims.
- Ingress principal headers are the established trust boundary; cannon does not
  independently authenticate a bearer token or query Brig client ownership.
  Preserve deployment assumptions rather than inventing new auth rules. Debug
  event/trace push logs include notification payloads: review serious privacy
  concerns and propose separate redaction, without changing the migrated contract.

Use established maintained Go libraries, pinned/checkable versions, verified TLS,
strict typed inputs, parameterized Cassandra, serialized socket writes and bounded
resources. No security bypass is accepted just to pass tests. The user's explicit
mirror-current-backend instruction applies to inherited behavior; new serious
findings must be recorded with source/runtime evidence and separate proposed fixes.

Review: codex (self-review delegated by user), same actor as author. The user
explicitly delegated review/autonomous development and requested completing cannon.
This inventory approves coverage/discovery only: no implementation, full e2e pass,
compatibility, availability, default change or production switch is claimed.
