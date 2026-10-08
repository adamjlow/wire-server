# Optional Go gundeck (in development)

The runnable implementation includes Redis-compatible presence, Cassandra
notifications, websocket/native delivery, SNS tokens, SQS feedback and consumable
RabbitMQ queues. Qualification is in progress under the whole-service task. This is not an available
replacement yet. Haskell remains supported/default; source contracts and fixture
oracle live in docs/go-migration/services/gundeck. No remote push or production
switch is performed.

Pinned Go1.26.8, one isolated module. Build cmd/gundeck and supply the existing
configuration with -c or --config-file; default /etc/wire/gundeck/conf/gundeck.yaml.
Redis master/cluster, credentials environment and verified TLS/customCA are
supported. Unsafe TLS verification bypass is rejected. Presence writes retain
schema/TTL and additional writes; old fields/URI values are readable; delivery
cleanup uses compare-before-delete. Presence API DELETE retains Haskell's no-op.
The whole-service acceptance matrix must pass before selecting this in deployment.

Retained core-2 evidence passes the unchanged frozen HTTP38 and storage13 captures.
Native40 and original72 mixed-service e2e, switchback and packaging remain required.

Response compression follows the [wai-extra default GzipSettings](https://github.com/yesodweb/wai/blob/master/wai-extra/Network/Wai/Middleware/Gzip.hs): MIME/negotiation, partial/existing encoding and explicit small-length guards.
Client IDs use the source-defined Word64 range and canonical lowercase/no-leading-zero encoding.
