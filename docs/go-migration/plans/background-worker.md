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
and all seven migration families. Scheduled jobs remain under construction.
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

## Current implementation and retained validation

The runnable notification pusher, dead-user watcher and Rabbit background-job
consumer implement source delivery, TLS, retry/ACK and group synchronization
contracts. Conversation storage, notifications, bot callbacks, administrative
PostgreSQL retries and Cassandra cleanup are connected. Readiness tracks the
three Rabbit concerns. These are construction claims; final whole-service parity
and operational qualification have not passed.

All seven optional migration families are connected: block lists, password resets,
conversation codes, domain registration, team features, users and conversations.
User copy preserves native SSO/scalars/assets, related service/handle/rich-info
reads, claim validation, Serializable PG transaction and pending deletion, then
Cassandra cleanup and ledger completion. Conversation migration starts the native
independent local-conversation and remote-membership scans, with typed paged IDs,
bounded per-page execution, exclusive locks and source timeout/retry rules.
Local ReadCommitted copy includes member status, remote members, ordered
subconversations and native MLS/history client handling. Per-table conflict rules,
NULL vs empty values, default access/role sets, inactive MLS data and cleanup
ordering follow the source. Pending cleanup retains the ledger on failures.

The latest construction checks are retained in
[local conversation evidence](../../../go/services/background-worker/evidence/local-conversation-migration-20261009/).
Pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/security/license checks pass
for its exact source-file identities. Focused tests cover concrete parsing, TLS,
transaction/cleanup, timeout, ordering and concurrency risks. Historical checkpoint
receipts describe their own tested inputs, not current availability or full parity.
No new dependency, Haskell/oracle/schema change or scanner suppression.

The early static executable matched Haskell health200/empty, unstarted readiness500
and SIGTERM exit0. This is limited process evidence, not final loaded operations.
The identified original gundeck40 run with only Go worker/Haskell peers passed40/40
on00557161c5218fba7ce290ba57536561298a35e3, with owned cleanup0 and empty project
labels. Earlier38/40 failure evidence remains retained. That passing run predates
later migrations and validates notification/DLQ/queue lifecycle only; it does not
qualify the current binary or scheduled worker.

The original users2 run on568810e637d656f08d54789b5d9408e83c5c53bc passed1/2.
Main migration case fails SCIM GET500 in runPhase2 before migrateUsers enables;
the reindexing case passes but never enables migration. Neither case exercised
the new user DB path. [Exact evidence](../../../go/services/background-worker/evidence/user-original-20261009/)
retains XML, build/environment identities, cleanup0 and empty project labels.
Resolve this failure before qualification; do not waive it or repeat unchanged
expensive tests without a repair. Actual migration effects remain unverified.

Scheduled payloads, Arbiter claims/heartbeat/retry/ACK/worker registration,
sibling locking/wakeup and archive retention are constructed. Schema setup,
reaper/cron, adminless/meetings domain handlers and scheduled runtime are unfinished.
Next validate the connected conversation path with the existing original MLS test,
then complete scheduled jobs and consolidate source startup/supervision/logging
and graceful drain. Final original47/exact six-Go124, actual durable
Haskell->Go->Haskell switchback, bounded operations, optional image/selector and
delegated same-author review remain mandatory. Native local Haskell build checks
remain unavailable/unrun. Service/task/goal remain active; Haskell stays default,
with no compatibility or availability claim.

Serious inherited concerns include PostgreSQL sslmode=prefer plaintext fallback,
transient/unconfirmed child-job publication, write-before-notification loss on
retry, poison-job requeue loops and bot pin authentication without hostname checks.
Native RSA-only bot TLS permits finite-DHE AEAD suites absent from Go TLS; providers
supporting only those suites have an unresolved interoperability limitation.
Native URI IPv6 serialization removes brackets and can produce ambiguous stored
URLs. Keep these findings separate from the parity implementation; no production
security policy change is authorized. Eventual distribution must include retained
email/URI BSD3 and Arbiter MIT notices.
