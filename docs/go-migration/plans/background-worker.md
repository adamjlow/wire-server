# Optional Go background-worker

The user selected background-worker on 2026-10-09 after documentation reconciliation.
Objective: complete an optional whole-service Go replacement, validated against
Haskell through a finite contract and the existing Docker harness. Continuing
explicit same-author review delegation applies; no production switch, remote push,
Haskell removal or contract/security exception is authorized.

Reference: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Current optional compiler:
Go 1.26.9, GOTOOLCHAIN=local/GOWORK=off. Existing five Go peers and their exact
[patched artifact tuple](../qualifications/go-patched-20261009/README.md) are the
starting mixed-deployment baseline. The service is inventoried. The finite reference contract is frozen with explicit baseline limitations;
no implementation or background-worker parity is yet claimed.

## Whole-service boundary to inventory

The entrypoint [Main.hs](../../../services/background-worker/exec/Main.hs) accepts
-c/--config-file and --galley-config-file, with existing separate background-worker
and galley defaults. Preserve both configuration inputs and their actual meanings.
[Wire.BackgroundWorker](../../../services/background-worker/src/Wire/BackgroundWorker.hs)
starts and coordinates these components:

- Federation backend-notification delivery and dead-user notification watching.
- RabbitMQ background-job consumption and PostgreSQL scheduled worker pools.
- Meetings cleanup and adminless/conversation jobs registered by the worker.
- Optional migrations for conversations, conversation codes, team features,
  domain registration, users, block lists and password reset, plus source-owned
  job/database setup. Inventory exact supported flags, schemas and resume rules.
- Health/readiness, metrics, retry/reconnect supervision, cancellation and shutdown.

Follow dependencies through libs and galley configuration; a small HTTP surface
is not evidence that the entire service is small. See
[Workers.hs](../../../services/background-worker/src/Wire/BackgroundWorker/Workers.hs),
[Jobs.Consumer](../../../services/background-worker/src/Wire/BackgroundWorker/Jobs/Consumer.hs),
[PostgresMigrations](../../../services/background-worker/src/Wire/PostgresMigrations.hs)
and the existing service unit tests. Resolve exact job registry, payload/queue
formats, acknowledgement/error rules, storage effects and migration behavior before
freezing acceptance. Do not substitute notification-only implementation for the
whole service or treat the existing 77 tests as coverage of unexamined jobs.

## Ordered execution and finite acceptance

1. Claim bounded discovery task. Create source-cited dossier inventory, trace all
   workers/config/storage/protocol paths, discover relevant original compiled test
   functions using the existing non-networked runner. Record absent executables,
   source/runtime mismatches and genuinely uncovered boundaries explicitly.
2. Freeze the finite contract/reference matrix in a separate contract task. Reuse
   original Haskell assertions in the existing owned dependency environment; add
   direct reference captures only where worker/job/migration/security/storage risks
   are not covered. Define bounded shutdown/load thresholds before measurement.
   Preserve protected reference fixtures/assertions/normalization. Separately scope
   required harness catalogue/selection extensions; six-service selection must
   identify every binary and fail closed on missing/duplicate/foreign targets.
3. Claim isolated go/services/background-worker module, keeping current Haskell
   interfaces, schemas and persisted payloads. Build an early runnable health and
   notification worker, compare directly with Haskell, then implement the remaining
   contracted jobs, migrations and supervision. No new shared Go framework or
   service/runtime split. Do not leave supported configured paths as silent no-ops.
4. Run appropriate format/tidy/verify/vet/race/unit/build/vulnerability/license
   checks and the frozen matrix against the final exact executable. Execute the
   relevant original Haskell tests with intended six Go services and remaining
   Haskell peers in owned private Docker networks. Reuse unchanged earlier service
   proofs; do not repeat all baselines or expand tests without a concrete gap.
5. Rehearse actual Haskell->Go->Haskell storage/queue switchback, including pending
   jobs, acknowledgement/retry/redelivery, lease/resume state and in-flight shutdown
   where applicable. Use one bounded operations smoke, not an open-ended benchmark.
6. Build/test optional nonroot image and default-preserving selector using existing
   chart/configuration. Verify actual lifecycle hooks and correct whole-process
   rollback. Delegated same-author review cites exact artifacts, failures and unrun
   checks. Publish legal adjacent service stages only after their gates pass; stop
   once finite acceptance is complete.

## Security and operating constraints

Use verified TLS with existing trusted fixture CAs; never disable chain/hostname
checks. Validate job types, payloads and remote domains using source contract,
bound parsing/concurrency/retries and preserve cancellation. Use parameterized SQL,
compatible serialization, transactional claims and source lease/idempotency rules;
never execute job input as commands or introduce eval. Maintain queue ownership:
no concurrent Haskell/Go consumers of the same production workload. Record serious
inherited data-loss, privilege, retry or trust-boundary concerns separately; mirror
current contracts without silently broadening privileges or inventing exemptions.
Select maintained dependencies only after actual need is known; pin/check security
and licenses and test concrete malformed input/auth/TLS/resource boundaries.

User authorization extends the existing task-owned synthetic Docker environment
for this service's relevant original tests/reference captures: internal networks,
no host ports, trusted TLS, synthetic data, owned volumes and guaranteed cleanup.
No shared/production targets. Preserve source schemas and Haskell build/defaults.
Native local GHC/Cabal/Nix are unavailable; record checks unrun, retain upstream CI.

Commands: make migration-check migration-test; tools/migration/check.py --base
FULL_SHA --task TASK; pinned compiler with python3 go/check.py background-worker
once the contracted module exists; existing tools/parity/docker controller/profile
with exact build receipts once separately reviewed selection support exists.
Build commands use required output filtering and inspect the actual build exit.
Evidence belongs in the service dossier/oracle and module review, with full source,
reference, binary, environment and cleanup identities. Open inventory questions
must be resolved before the contract is called complete; no guessed test counts.

Inventory progress: actual pinned runner lists 47 candidate relevant original
functions including 15 migration, 23 adminless, 3 group synchronization,meeting
cleanup, offline federation/dead-letter/queue recovery. Published worker help
confirms separate galley config; Haskell worker SHA985aab36... recorded. Two
Rabbit job payloads synchronize user groups/channels; scheduled PostgreSQL jobs
cover adminless setup/deletion/reminder and meetings. Inventory identifies whole
source boundary and concrete risks; finite reference contract and actual Arbiter schemas were then frozen before
Go construction.

Inventory publication: the completed [inventory](../services/background-worker/inventory.md)
and [actual discovery](../services/background-worker/test-discovery.json) are
recorded by the inventory gate; the completed reference contract covers the
original47, actual persisted queue/schema behavior and strict six-service selection.

Reference work completed: all original47 functions executed; full runs45/47 and
46/47 failed and are retained. Case-level passing observations cover47/47 across
identified runs; isolated five-Go MLS passed twice without a proven repair.
Original-coverage.json explicitly disclaims a whole-suite green baseline. The
existing-peer intermittent transport issue stays open with compatibility null;
it does not alter the independently frozen worker contract. Final exact six-Go
original124 tests must still pass before qualification. Proceed to the early
runnable worker and whole contracted implementation; do not repeat broad baselines
without a concrete change. Final switchback remains outstanding.

Early construction: isolated Go module accepts both configs and CLI defaults,
source health/ordered readiness and federation pusher. Single-active quorum
consumers preserve raw bytes, V0/bundle version rules and ACK/reject/retry/KeepQueued
behavior; verified TLS and cancellable bounded AMQP handshake. Actual focused
HTTP2 transport/source expectation tests plus format/tidy/verify/vet/race/build,
module vulnerability and license checks pass. The initial missing local module
LICENSE failure is retained; corrected source includes the original AGPL license.
The harmless license-report HEAD URL warning is not a compiler warning; actual
local license content was classified AGPL and dependencies BSD/MIT. DLQ/group-sync,
Arbiter/adminless/meetings and seven migrations remain unfinished; their readiness
stays false and enabled migrations explicitly fail startup. No availability,
whole-service parity, final operations or image/selector claim is made.

The exact static early executable from bdfb252dc3e1a87c14eb282ef6982e3d3194548b
matched actual retained Haskell health200/empty, explicitly returned500 for
unstarted workers and SIGTERM exited0 in0.0011s. This is a limited early process
comparison, not source14 native-suite execution, E2E, an operations smoke or
switchback. Receipts are module-owned; protected captures unchanged. Next build
DLQ watcher and Rabbit group-sync/storage paths, then Arbiter/business jobs and
all migrations. Refine source discovery retry budget, draining and strict dual
configuration while completing those paths; no unsupported configured path may
remain at final qualification. Maintain all final original tests and durable
rehearsal; do not shrink to the functioning notification/health slice.
