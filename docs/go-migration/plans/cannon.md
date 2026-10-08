# Optional Go cannon

Goal authorized by the user on 2026-10-08: complete cannon, then validate Go
proxy/cargohold/cannon together with remaining Haskell peers. Keep Haskell the
default, preserve upstream source/configuration/storage/protocols, and mirror
existing behavior while recording serious security improvements separately.
Reference/upstream source: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Reuse the
qualified immutable publisher runtime and private Docker dependency environment.
This user instruction extends that isolated test workflow to cannon; shared and
production targets, deployment switching and additional pushes are excluded.

## Ordered work and finite acceptance

1. Inventory public/internal routes, classic WebSocket and consumable-event
   protocols, gundeck presence, RabbitMQ queue/ack/pool and Cassandra missed-event
   queries. Discover the original compiled Events, Notifications and useful Demo
   tests in the pinned container; freeze their actual required-case catalogue.
2. Source-review/freeze the contract and only capture concrete ambiguities with
   real Haskell in the existing private synthetic environment. Run the original
   selected tests on Haskell before candidate use. Advance inventory/contract/plan
   gates through serialized scoped coordination, without a compatibility claim.
3. Build one additive Go 1.26.8 module. Start with classic await/internal push and
   gundeck presence, compare immediately with real Haskell, then implement events
   RabbitMQ delivery/ack/sync and Cassandra recovery. Select established pinned
   WebSocket/AMQP/Cassandra libraries only after version/license/vulnerability
   checks. Do not translate Haskell abstractions or create a generic framework.
4. Reuse original assertions against Go cannon with Haskell peers. Add bounded
   differential checks only for uncovered API/frame/security/error/config and
   shutdown boundaries below. Package an optional non-root image and selection.
5. Run relevant original cannon tests plus original proxy 5/cargohold 27 in one
   three-Go environment. Validate persisted queue/Cassandra switchback across
   runtimes, perform delegated same-author review and record exact tested tuple.

| Surface | Bounded acceptance |
| --- | --- |
| Classic sockets | await/websocket aliases, client/principal headers, binary push/raw JSON, bulk target status, missing/remote presence, duplicate connection cleanup, application and control ping/pong |
| Consumable events | v8/v9 routing, temporary/client queues, binary event and delivery-tag shapes, single/multiple acknowledgements, prefetch, single-consumer behavior, malformed/invalid ack close codes |
| Recovery/effects | unacked redelivery after reconnect, temporary queue deletion, expired/transient events, missed-notifications full-sync ack and Cassandra deletion, synchronization marker ordering |
| Errors/security | malformed/missing headers/client/JSON, wrong user/client isolation, unavailable gundeck, RabbitMQ channel limits/failure, disabled versions; exact observed statuses/close codes and relevant provider effects |
| Operations | one bounded 40-request/socket smoke, concurrency 2, Go RSS <=192 MiB, p95 <=2*Haskell+100 ms; health, config/CLI, metric/log shape, inactivity and loaded SIGTERM drain under 2*fixture grace+2 seconds |
| Selection/switchback | omitted selection Haskell, explicit Go image, invalid selection fails; Haskell-to-Go and Go-to-Haskell queue/Cassandra state recovery, all three Go services with Haskell peers |

Original tests supply these assertions wherever available; do not duplicate them
with another custom suite. Freeze uncovered supplemental stimuli/normalization
before candidate use. Match semantic responses/frames, ordering, storage/queue
effects and meaningful negative cases; normalize only validated generated IDs,
clock bounds and incidental runtime diagnostics. Stop at the matrix once it
passes. Threshold changes require source/reason review before rerunning.

## Security, evidence and commands

Go 1.26.8; secure maintained libraries, verified TLS, standard crypto/randomness,
strict typed input, parameterized Cassandra queries, context/resource bounds,
serialized WebSocket writes and generic contract-compatible errors. No secrets,
customer fixtures, new auth trust boundary or bundled security hardening. Record
serious inherited concerns with source/runtime proof and proposed separate fixes.

Use tools/parity/docker/control.py build/preflight and environment.py prepare/run
with new external cache outputs. Extend bounded service selection/catalogue only
where needed; do not change original Haskell assertions. Keep pinned runtime,
compiler/source/binary/config/dependency hashes and sanitized original XML/run
receipts. Private internal Docker networking, no published ports, synthetic data,
owned cleanup; never prune unrelated resources. Native Haskell tools are currently
unavailable; unchanged source retains upstream CI, not fabricated local checks.

Candidate checks: PATH containing pinned Go, python3 go/check.py cannon; focused
race/security tests during work. Framework checks: make migration-check
migration-test; python3 tools/migration/check.py --base ACTUAL_FULL_SHA --task ID
and git diff --check. Scope each task before writes. Implementations cannot alter
the protected oracle. Record user-delegated review honestly as the same actor.

Rollback is an operator-selected whole Haskell process/image using unchanged
queues, schemas and gundeck contracts. No production switch is authorized.
Unresolved behavior is characterized with the real reference, not guessed or
normalized away. The plan is coverage intent; only actual retained runs are proof.
