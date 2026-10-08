# Gundeck observable contract

Status: approved frozen reference for implementation under delegated self-review.
Reference/upstream f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Scope is the whole
optional gundeck process; Haskell remains supported/default. The source-cited
[inventory](inventory.md) and [finite plan](../../plans/gundeck.md) define routes,
provider/storage effects and limitations. No schema/auth boundary or production
switch is authorized. Same-author review uses explicit user delegation, not an
independent reviewer label.

## Reusable original assertions

The pinned compiled integration runtime actually discovers40 original cases in
[the additive catalogue](../../../../tools/parity/docker/gundeck-tests.json):
original35 Events/Notifications/Demo plus PushToken registration/VoIP rejection,
MLS pagination/oversize-since and Cells ignored events. Original test code and
assertions are untouched. Native Haskell legacy gundeck-integration executable is
absent; its source informs uncovered stimuli but is not claimed executed.
[Original all-Haskell40](oracle/baseline.run.json) passed, no failures/errors/skips,
with [XML](oracle/baseline.xml) and [process0/owned cleanup0](oracle/baseline.environment.json).
Four-Go selection requires exact proxy/cargohold/cannon/gundeck identities; final
combined runner uses proxy5/cargohold27/gundeck40 once (72 original cases), retaining
all original35 cannon assertions without executing them twice.

## Frozen uncovered HTTP/presence and seeded storage

[HTTP/presence38](oracle/http-presence-reference.json) and its
[receipt](oracle/http-presence-reference-run.json) capture real Haskell responses
and verified-TLS Redis effects. Exact unique record names and all semantic fields
must match: HTTPstatus, Content-Type, Allow, Location, structured or text body,
presence resources/client/device identity and legacy value parsing. Current UTC
time and millisecond presence timestamp are validated within10s then normalized;
seven-day Redis TTL is validated604790..604800 to allow execution time. Redis
unordered legacy hash listings sort by device_id; no array-order normalization
for notification delivery/storage. Missing Content-Type still accepts presence
POST. DELETE presence succeeds and preserves the row; no eager-delete exception.

[Seeded storage13](oracle/storage-reference.json) and its
[receipt](oracle/storage-reference-run.json) pin own synthetic rows and actual
private Haskell process/config/binary. Inline/referenced JSON, filtered last scan,
missing/malformed/null payload skips, wrong-client404, since filtering, has_more,
byte-bound paging, user cleanup and subsequent empty reads remain exact. Fixture
internalpage2/maxpayload25 intentionally expose byte truncation; original paging
assertions cover the normal larger page. Prepared production queries must preserve
existing schema/consistency, TTL and timeuuid ordering. Fixture CQL is fixed
synthetic literal setup, never accepted caller data; production uses parameters.
The side process is always reaped and own fixture notification/payload rows removed.

## Delivery/provider effects

Mandatory scope remains persistence before asynchronous delivery, origin/client
selection, Redis outage and generation-safe cleanup, additional Redis writes,
RabbitMQ persistent/transient expiration and Cells routing, native token sharing/
replacement/error mappings, SNS notice envelopes and SQS feedback acknowledgement/
cleanup. Actual original40 supply event delivery/persistence/deadletter/transient/expiry/
registration, Cells and notification paging assertions. Source-defined queue names,
properties/routing remain Wire.API.Notification and Gundeck.Run/Client/Push; reuse
unchanged cannon shared queue source/reference proof rather than duplicate every
original event assertion. [Native/config/ops40](oracle/native-reference.json) actual
Haskell capture supplies typed token error responses, shared owner metadata and
replacement migration, GCM high/low and APNS envelopes, disabled publish cleanup,
TTL/permanent feedback and shared cleanup/deleted receipts. Its [review](oracle/native-reference-review.md)
defines exact semantic record comparison and narrowly incidental raw AWS request
entry ordering/polling/notice UUID normalization. SQS malformed events retain their
receipt; delete occurs only after valid decode and successful callback (Aws.listen);
unknown/TTL feedback retains token state. Candidate security tests must verify this
source-defined failure behavior. Private init provisions synthetic SNS applications
and SQS queues; original PushToken assertions passed against that setup. No production
AWS certification is claimed.

Source correction to discovery: `Push/Native.hs.publish` calls
`Aws.publish endpoint payload mempty`; **SNS message attributes are empty**.
The inventory's statement about passing notificationTTL to SNS was inaccurate.
TTL applies to Cassandra and RabbitMQ messages; do not invent SNS TTL attributes
in the Go implementation. GCM priority and APNS payload bytes follow
`Push/Native/Serialise.hs`; token/error/feedback source paths remain in inventory.

## Operational/security disposition

Keep certificate chain/hostname verification and parameterized storage access,
strict typed input, exact integer values, bounded resources and generic contract
errors. Unsafe Redis or broker TLS-bypass options are explicitly unsupported in Go;
retain Haskell for such deployments. No unsafe security exception is approved.
Serious source concerns: whole push payload debug logging, token values in lifecycle
logs, optional unbounded native concurrency, TLS bypass configuration, and shutdown.
Track proposed fixes separately; do not bundle unrelated hardening or claim unsafe
reference behavior approved merely by delegated review.

Actual isolated Haskell SIGTERM exits1 in0.36s with
[an MVar deadlock diagnostic](oracle/storage-shutdown-reference.log). This is an
inherited cleanup failure, not a passing clean shutdown. Runtime-specific exit and
internal diagnostic text are not API identity targets: the Go candidate must prove
clean exit0<=4s, plus one bounded in-flight push shutdown. Keep the failed first
capture in history/receipt and do not relabel it pass. One40request/concurrency2
smoke must preserve p95<=2*reference+100ms and RSS<=192MiB. Native40 additionally retains gzip request decoding, five required-field startup
rejections, CLI config/default-path help, exercised counter names/types and one
40-request smoke (reference RSS42888KiB/p951.79ms). Haskell counters are lazy:
required exercised types match; additional zero counters may be exposed. Existing
source-defined logformats/disabled API version/gzip response behavior must be preserved
and tested in candidate negatives. Loaded bounded shutdown remains a candidate
operational check; reference idle native fixture exits0 in0.008s while seeded
storage fixture exposed an intermittent MVar exit1. No candidate operational gate
is passed by reference evidence alone.

Completion requires exact final source/binary/module/security proof, full original
four-Go72 pass with remaining pinned Haskell peers, finite uncovered comparisons,
real Redis/Cassandra/token/queue Haskell-to-Go and Go-to-Haskell switchback, optional
nonroot Docker image and one-service Helm selector preserving Haskell defaults.
No local native Haskell compile/unit check is claimed; unchanged source retains CI.
Future upstream changes invalidate old compatibility until requalified.

Dependency choice: [pinned decision and actual module vulnerability/license proof](oracle/dependency-decision.json). Use Go1.26.8, maintained Redis/AWS SDK modules and standard SQS Query/XML with SDK SigV4 to preserve configured provider compatibility; [AWS documents both Query and JSON support](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-json-faqs.html). No custom authentication crypto or obsolete SDK. Final whole module must pass pinned checks.
