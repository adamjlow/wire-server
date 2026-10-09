# Optional Go background-worker

The user selected background-worker on 2026-10-09 after documentation reconciliation.
Objective: complete an optional whole-service Go replacement, validated against
Haskell through a finite contract and the existing Docker harness. Continuing
explicit same-author review delegation applies; no production switch, remote push,
Haskell removal or contract/security exception is authorized.

Reference: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Current optional compiler:
Go 1.26.9, GOTOOLCHAIN=local/GOWORK=off. Existing five Go peers and their exact
[patched artifact tuple](../qualifications/go-patched-20261009/README.md) are the
starting mixed-deployment baseline. The service is inventoried and its finite
reference contract is frozen with explicit baseline limitations. Go construction
now includes a runnable notification pusher and Cassandra dead-user watcher, plus
connected background-job consumption, conversation-storage/notification adapters
and five migration families. Scheduled jobs and the other two migrations are
under construction.
Whole-service parity is not yet claimed; the task record owns current status.

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

Use verified TLS with existing trusted fixture CAs for backend dependencies.
Registered external bots use the source RSA public-key pin contract: a matching
pin plus valid CA chain or valid self-signature and dates. Native bot verification
does not require a hostname match; document that inherited concern and retain
mandatory authentication rather than exposing an unchecked TLS client. Validate job types, payloads and remote domains using source contract,
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

Current implementation and retained validation:

- The notification pusher and dead-user watcher run; other worker readiness stays
  tracked for all three Rabbit concerns. Enabled database migrations explicitly
  reject startup while unfinished.
  Source HTTP2 delivery, version rules, retry/ACK behavior, verified AMQP TLS,
  missed-notification writes and malformed input are covered by focused tests.
- Rabbit background-job parsing/session/retries/metrics and both group-sync
  algorithms are implemented. PostgreSQL conversation/member reads and writes,
  administrative-shutdown retries, local Gundeck pushes and modern/legacy remote
  notifications are implemented. The real job consumer is now connected with a
  separate publisher channel and all three conversation storage modes.
- Cassandra conversation/member reads, bound logged membership batches, reverse
  indexes and the source year3000 deletion tombstone are implemented. Nullable
  decoding explicitly distinguishes NULL from scalar zero; default driver map
  scans otherwise lose this distinction. Immediate x1 and deterministic x5
  retry timing follow the source.
- Bot callbacks use real Cassandra service lookup, independent process lifetime,
  Bearer-authenticated HTTPS and native RSA fingerprints. The fingerprint test
  uses the existing public mock-service fixture and an independently computed
  OpenSSL BN_bn2mpi digest. Real loopback TLS tests cover valid pinned self-signed
  delivery, wrong pins, expired/tampered certificates, source callback path,
  attempt cancellation, disabled/missing services and finite HTTP retries.
  These are construction checks, not actual original group/database E2E proof.

The early clean static executable from bdfb252dc3e1a87c14eb282ef6982e3d3194548b
matched retained Haskell health200/empty, returned500 for unstarted workers and
SIGTERM exited0 in0.0011s. This limited process comparison is retained in
[module evidence](../../../go/services/background-worker/evidence/early-20261009/).
It is not the final loaded operations smoke or durable switchback.

The original owned gundeck40 run with only Go worker and remaining Haskell peers
is terminal: strict XML40 cases,38 pass,2 fail and cleanup0. The queue-deletion
case encountered a wrong federation version header before testing recovery;
6198c021681272d66c57b32633594a4487847b81 corrected the candidate to native decimal
versionInt, including unversioned0. The other failure was the missing
background-jobs queue from the unfinished consumer. Retained
[watcher evidence](../../../go/services/background-worker/evidence/watcher-20261009/)
identifies the exact failed artifact and actual execution. The controller's outer
false execution flag denotes failed qualification; inner receipts/XML establish
actual suite execution. Both cases now pass in the unchanged original40 rerun against clean source
00557161c5218fba7ce290ba57536561298a35e3, with only Go worker and Haskell peers.
Strict XML40/40 and owned cleanup0 are retained in module evidence/jobs-original-20261009.
The original failure history remains valid for its earlier exact artifact.

Partial check/source receipts are retained in module evidence/group-jobs-20261009,
conversation-adapters-20261009, cassandra-bots-20261009 and migration-routing-20261009. They do not establish
whole-service parity or a final executable. Protected original assertions and
schemas are unchanged. No availability/image/selector/compatibility claim is made.

Conversation routing now uses source shared UUID advisory locks, a500ms acquisition
budget and pending-delete cleanup before reads/writes. Incomplete MLS writes route
by PG EXISTS rather than read success. Failed Cassandra cleanup retains the PG
ledger; cleanup loads native subgroup/group/client data before deletion. Focused
routing/failure tests and full Go checks/final race tests pass. Actual database
lock/durable effects are not yet verified in Docker.

The unchanged original gundeck40 rerun passed40/40 against clean worker source
00557161c5218fba7ce290ba57536561298a35e3. It includes real job startup and migration
routing, but no subsequent scheduled construction. This is notification/DLQ/queue
lifecycle proof; original group business/storage, worker47 and final six-Go124
remain outstanding. Actual owned container/network/volume cleanup was verified.

Scheduled construction now has source queue-specific payload decoding and native
gateless single-job claim, heartbeat and retry SQL with attempt/lease guards.
Worker registration/heartbeat with effective worker/queue pause and shutdown/remove
methods are also constructed. Original Arbiter MIT notices are retained. Full Go
checks/final format-vet-race pass; actual PG behavior
and the scheduled runtime are not yet tested/connected. Evidence is in module
evidence/scheduled-store-20261009. Complete schema bootstrap, worker registration,
ACK/archive/reaper/cron and domain handlers before enabling scheduled execution.

Next consolidate startup/supervision/logging and graceful drain. Implement Arbiter
scheduled adminless/meetings jobs and all seven migration
families. Final exact six-Go original124 tests, actual durable Haskell->Go->Haskell
switchback, bounded operations, optional image/selector and delegated review remain
mandatory. Native local Haskell build checks remain unavailable/unrun.

Serious inherited concerns include PostgreSQL sslmode=prefer plaintext fallback,
transient/unconfirmed child-job publication, write-before-notification loss on
retry, poison-job requeue loops and bot pin authentication without hostname checks.
Native RSA-only bot TLS also permits finite-DHE AEAD cipher suites that Go's TLS
library does not implement; legacy providers supporting only those suites may
need configuration updates. No production security policy change is authorized.

Block-list, password-reset, conversation-code, domain-registration and
team-feature migration loops are now constructed and connected, with source typed-page decoding, TTL/legacy handling,
bound upserts, full rescans on PG usage errors, fatal timeout and metric behavior.
Stored password hashes are canonicalized without KDF work. Native CQL ASCII text
bypasses API alphabet validation; the candidate now preserves that distinction
and native code-length ranges. The other two enabled migration flags still
reject startup explicitly. Scheduled optimistic ACK, parent
sibling locking/suspension/wakeup and archive retention are also constructed.
Full Go checks pass for module evidence/domain-feature-migrations-20261009 inputs;
actual migration DB behavior and scheduled ACK remain untested, and scheduled
runtime remains disconnected. Final original124/operations/switchback are required.

Domain challenges and registrations follow the source ordered two-phase scan,
with independent lock acquisition/row timeout budgets and current-row reread
under an exclusive lock. Team features preserve existing PG rows, nullable patches
and generic JSON configuration. Both use native hashable1.5.1.0 XXH3 UTF8 lock keys;
independent C hash vectors are retained with the source citation. Stored HTTPS
URL serialization follows the native URI library, whose BSD3 notice is retained
alongside Arbiter MIT. Native IPv6 serialization removes brackets and may produce
invalid/ambiguous stored URLs; this inherited data-format concern is documented.
Native library execution and actual migration DB effects remain unrun.
