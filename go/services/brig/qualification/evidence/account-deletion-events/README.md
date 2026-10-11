# Account deletion event fanout

Source `cc80e031bd640cdd717f4c517d3a2db2a1058234`, static service
`e9433b45e6465b2d9f253caf9344ae50ed365aac8fb419f63e068ebf595feb65`:
[full pinned Go checks](go-check.log) and
[unchanged original-runner static units](runtime-units.log) pass.
[Receipt](build-receipt.json) identifies compiler/images/archive manifest and
exact service/unit/tagged-store artifacts. Tagged stores compiled only;
no new native schema, real queue or original integration execution here.

Native references: `Brig.IO.Intra.notifyUserDeletionLocals/Remotes`,
`Brig.Data.Connection.lookupLocalConnections/lookupRemoteConnectedUsersPaginated`,
`Wire.NotificationSubsystem.Interpreter`, federation BackendNotifications and
HasNotificationEndpoint, Brig notification payload, UserEvents.proto and
`Brig.IO.Journal.userDelete`. Preserve synchronous contacts before row cleanup,
local legacy501-read/500-trim/native has-more cursor and opaque remote1000 pages.
Direct self/accepted-contact user.delete has qualified_id and id. Raw Sent only
gets reversed Cancelled with fresh milliseconds, original conversation and no
name/origin connection. Missing-legalhold history is not treated as raw Sent or
Accepted. Accepted pushes split128 recipients before per-chunk set ordering.

Remote domain buckets sort domains and preserve contact order. Native bundle
uses string-encoded body, component brig, on-user-deleted-connections, ownDomain,
requestId, versions from0/no upper bound and keep_queued. Required enqueue succeeds
before best-effort RemoteRescind, and optional USER_DELETE journal is last.
Unit controls demonstrate these wire/order/cursor/chunk/failure rules and prevent
effects when required dependencies are absent. The persistent enqueue callback
has no successful default; this does not substitute direct RPC for a real queue.
Concrete Rabbit publisher/settings/lifecycle and live queue proof remain required
before the engine is publicly bound. Existing raw store decoder validates relations.

Context-aware callbacks, bounded pages/chunks, domain validation and existing
parameterized store queries preserve resource/input boundaries. No new dependency
or security policy. [Cleanup](cleanup.json) confirms terminal controller and
independent empty owned resources. [Cache maintenance](cache-maintenance.json)
removed only idle owned compiler-cache entries, never retained artifacts.

Same-author scoped review under the user's prior delegation accepts this event
increment, not whole Brig. Public DELETE/self still incomplete; no original22,
whole535/eight-Go, operations, switchback, image or service gate advanced.
