# Gundeck whole-service inventory

Inspected reference/upstream f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e.
Go replacement is a separate opt-in whole process. Haskell source/build/charts,
Cassandra schemas and external caller contracts remain unchanged.
[Executable finite plan](../../plans/gundeck.md) includes explicit native-push
and feedback coverage; this inventory is discovery, not candidate qualification.

## API and routing

Authoritative declarations: `libs/wire-api/src/Wire/API/Routes/Public/Gundeck.hs`
and `Routes/Internal/Gundeck.hs`; handlers `services/gundeck/src/Gundeck/API/{Public,Internal}.hs`.
Public token POST/GET/DELETE under `/push/tokens`, notification list/by-ID/last,
server time fromv9. Z-User principal and Z-Connection token registration remain
trusted gateway inputs; do not invent new service authentication.
Untilv3 invalid/missing since can return a 404 with a populated notification page;
v3+ gap returns notification-not-found. Pagination requires V1 timeUUID, defaults
size1000, accepts100..10000, updates brig client activity when client specified.
`Notification.hs.paginate` and `API/Public.hs.paginateUntilV2` contain these details.

Internal `/i/status`, POST `/i/push/v2`, presence GET uid/GET ids/POST/DELETE,
DELETE `/i/clients/:cid` and `/i/user`, GET `/i/push-tokens/:uid`, POST
`/i/users/:uid/clients/:cid/consumable-notifications`. Presence POST intentionally
ignores Content-Type (`ReqBodyHack`); response201 Location is supplied resource.
**Presence DELETE is currently a successful no-op** (`Presence.hs.removeH`);
implementing eager deletion would change the contract. Consumable registration
is a no-op when the feature is disabled.

`Run.hs.middleware`: API version gating, request ID, Servant Prometheus metrics,
request gunzip/response gzip and exception mapping. Need characterize compression,
missing/wrong types, precise errors and disabled routing before candidate use.

## Storage and side effects

`Presence/Data.hs`: Redis hash `user:<uuid>`, current field connection bytes and
JSON `{r:resource,c:optionalClient,t:milliseconds}`; HSET+EXPIRE transaction sets
whole user's seven-day TTL. Additional Redis writes are supported. Reads default
Redis, skip invalid values, accept old raw-http URI values and `connection@cannon`
fields, preserve original `__field`. Conditional removal WATCH/HGET/MULTI compares
full stored presence to prevent deleting a replacement. Do not simplify legacy
migration or removal into unconditional HDEL.

`Notification/Data.hs`: parameterized LocalQuorum notifications/payload queries.
One target stores inline JSON payload; multiple targets share random UUID payload
reference plus byte size. Both rows use existing TTL seconds. Notifications keyed
by user/timeUUID; ASC pages and DESC last scan, client set empty meansall; absence
of queryclient still returns targeted rows for compatibility. Skip malformed or
missing payloads, default internalpage100/maxpayload5MiB, always load first row
within a page despite size, track gap/has_more and avoid inventing simpler paging.
Reads retryx1, writesx5; referenced payload fanout16, writefanout32. User deletion
removes notifications and token rows; payload rows expire via TTL.

`Push/Data.hs`: `user_push(usr,transport,app,ptoken,arn,connection,client)` schema;
legacy rows missing client/ARN are deleted during lookup. Token reads use selected
consistency (native lookupOne, public/internalAll orLocalQuorum), writesLocalQuorum.
Client unregister deletes associated tokens; user removal cleans shared SNS endpoints
through `Push/Native.hs.deleteTokens` before erasing own rows.

## Delivery and broker

`Push.hs.pushAll`: brig `/i/clients/full` batches100/concurrency4 provides client
capabilities. Split mixed legacy/consumable recipients without dropping no-client
legacy fallback; generate separate notification IDs for legacy, queue and Cells
paths. Persist nontransient legacy pushes before asynchronous websocket/native
processing: Redis outage must not prevent storing notifications. Websocket targets
exclude exact origin user+connection and enforce client list only when presence
has a client. Successful websocket deliveries and opposite-generation clients
suppress duplicate native pushes. Direct route suppresses native delivery;
transient events are neither persisted nor natively pushed.

`Push/Websocket.hs`: group existing presence resources by cannon, ordered bulk
response accounting; distinguish gone versus unreachable presence and conditional
Redis cleanup. Preserve these provider effects rather than new push architecture.
`Push.hs.mkMessage`: queued notification `{id,payload}`, JSON content type,
persistent versus nonpersistent, TTL milliseconds versus transient expiration0.
User/client direct-exchange routing keys and optional Cells default-exchange queue
must match `Wire.API.Notification`; no extra transient field in broker body.
`Client.hs.setupConsumableNotifications`: existing quorum queue options and two
bindings (user/client). `Run.hs` declares user+deadletter direct exchanges, quorum
DLQ/binding and optional quorum Cells queue at startup. Exact effective durability,
arguments, publisher properties and failure outcomes need captured reference proof.

## Native provider and feedback

`Push.hs.addToken`: APNSVoIP registration rejected; token transport/app/client
matching replaces overlapping tokens while retaining other apps/clients. SNS
create endpoint, existing-endpoint lookup/update/recreate (three-inconsistency
limit), shared user metadata and endpointenabled handling; API maps app-not-found,
invalid/too-long token, metadata-too-long and exhausted thread budget distinctly.
Deletion must preserve endpoints shared by other users or replacement tokens.

`Aws.hs` uses Amazonka signed SNS/SQS protocol with configured endpoints/account/
region/environment, discovered credentials, SNS timeout5s and AWS retries. Startup
resolves SQS queue URL. Receive longpoll20s/max10; empty poll throttle defaults500ms.
Need compare actual synthetic protocol requests and feedback acknowledgement.
`Push/Native/Serialise.hs`: platform-specific JSON-string envelope, notice id/user,
GCM prioritynormal/high, APNS alert+mutable-content, transport byte limits4096/5120.
`Push/Native.hs.push1`: success/disabled/invalid/too-large/unauthorized/error counters;
disabled/invalid token cleanup emits push-remove and stored notification, unauthorized
endpoint recreates/retries once. Native push passes notificationTTL to SNS.
`React.hs`: endpointupdated supersedes tokens; permanent/invalid failure removes
endpoint/tokens, disabled failure cleans only when disabled; TTL/unknown failure
logs without deletion; missing endpointignored, orphan endpointdeleted. Token removal
sends/stores client-targeted push-remove. These are mandatory whole-service effects.

## Configuration, operations and security

`Options.hs`: bind/brig/Cassandra/Redis/additionalRedis/AWS/RabbitMQ/discoUrl,
settings(notificationTTL/httpPoolSize/nativehard-softbudget/perpushconcurrency/
SQSthrottle/disabledversions/payloadpagebytebounds/Cellsqueue/consumabletoggle),
logging. Redis master/cluster and verifiedTLS/customCA are supported options;
unsafe TLS bypass must remain explicitly unsupported under repository policy.
`Run.hs`: startup schema version, broker setup, feedback/budget/auth metric workers;
HTTP shutdown cancels workers and closes Cassandra/Redis/logger. CLI/configdefault,
metric labels/logformat and bounded in-flight shutdown need reference capture.

Serious inherited concerns: debug logging includes whole push payload (`Push.hs`),
token values/identifiers appear in token lifecycle logs (`Push/Data.hs`, `Push.hs`),
Redis TLS verification bypass option (`Options.hs`), unbounded per-native concurrency
when unset and async push work during shutdown. Record evidence and proposed fixes
in contract; do not bundle security changes or claim unsafe-mode parity. Use only
synthetic fixtures, verifiedTLS, bounded Go contexts/resources, prepared queries.

## Original tests and concrete gaps

Actual no-network read-only pinned runner discovery on2026-10-08 (image
sha256:f88496cd7850c4a32563e2085d41d2647e1038682a61fe0ffb5540c07f88ad7b)
listed all existing Events cases (including both typingindicator variants), seven
Notifications cases, `PushToken.testRegisterPushToken`,
`PushToken.testVoipTokenRegistrationFails`, MLS notification pagination/oversize,
consumable feature/client capability and Cells ignored events. The legacy
`gundeck-integration` executable is absent from PATH. Discovery ran no tests;
original unchanged compiled catalogue must be frozen in a separate contract task.
The original35 cannon reference reports are reusable for unchanged runtime/fixtures,
not a native/provider gundeck baseline or candidate pass.

`services/gundeck/test/integration/API.hs` legacy source tests cover paging/client
filtering/presence replacement/stale cleanup/Redis outage/migration; RealAWS group
covers token sharing and limits. Reuse compiled current tests where available;
characterize legacy-source uncovered behaviors with real Haskell synthetic requests,
not a duplicate giant suite or claim of executing unavailable legacy tests.
Private existing environment provides SNS alias via fake_localstack and SQS;
platform application setup and exact SNS feedback behavior are **not yet validated**.
Next contract work must establish that setup and baseline PushToken tests, capture
finite provider negatives/SQS effects plus Redis/Cassandra/page/version boundaries,
and extend harness selection to gundeck/fourGo. No production AWS is required.
First candidate slice: health + Redis-compatible presence + notification retrieval
against real initialized private Cassandra/Redis, then full push/native/feedback.
