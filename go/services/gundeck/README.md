# Optional Go gundeck (in development)

The first runnable increment supports health, Redis-compatible presence and server
time. Notification storage/retrieval, delivery, native/SQS and final qualification
are being completed under the active whole-service task. This is not an available
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
