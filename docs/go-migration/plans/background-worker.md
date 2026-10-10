# Optional Go background-worker

The user selected background-worker on 2026-10-09 after documentation reconciliation.
Objective: complete an optional whole-service Go replacement, validated against
Haskell through a finite contract and the existing Docker harness. Continuing
explicit same-author review delegation applies; no production switch, remote push,
Haskell removal or contract/security exception is authorized.

Reference: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Current optional compiler:
Go 1.26.9, GOTOOLCHAIN=local/GOWORK=off. Existing five Go peers and their exact
[patched artifact tuple](../qualifications/go-patched-20261009/README.md) are the
starting mixed-deployment baseline. The complete service now passes the finite acceptance matrix. See the
[final same-author review](../../../go/services/background-worker/REVIEW.md)
for exact artifacts, scope, reused evidence and limitations.

## Final validation checkpoint (2026-10-10)

Candidate source `5a24af6f27c622f62ce8cdab35923d6acc765a2d`, static worker SHA256
`9a3157e2f61a3406e4923b1a5e569f2ad0f03e50ccdad25b4c7f43d22f3bc658`,
image `sha256:965c576532a1dc0c3a7a31be9cfa6267e2185f93847e9a0ef7379e3f1aecf68d`.
All production code remains unchanged after that source revision.

- [Current original integration receipts](../../../go/services/background-worker/evidence/final-original124-20261010/): all124 executions pass with six identified Go services and remaining Haskell peers; worker47, proxy5, cargohold27, gundeck40 and federator5. There are120 distinct functions/four Events overlaps. Strict named XML, no errors/skips/failures; process/controller/owned cleanup0 and no remaining resources.
- [Current Go checks and response repair](../../../go/services/background-worker/evidence/notification-response-20261010/): pinned format/tidy/verify/vet/unit/race/build/vulnerability/license pass. Actual switchback exposed a concrete notification ACK mismatch; source Post200/JSON/object acceptance was repaired with failing-before/passing-after retry tests.
- [Current operations](../../../go/services/background-worker/evidence/operations-final-20261010/): 100health/concurrency8 plus10jobs in0.597s, counters/ACK/busy0, sampledRSS/CPU/latency. Exact nonroot image/default mounts/trusted broker TLS and loaded SIGTERM0/.0577s preserve both unsent notifications; cleanup0.
- [Actual durable switchback](../../../go/services/background-worker/evidence/switchback-final-20261010/): same Rabbit/PG/Cassandra Haskell→Go→Haskell state, native attempt1 reclaimed as Go attempt2/newowner, real scheduled effects, partial migration continuation, return-native new migration/job and both notifications ACK. The first native worker cannot stop cleanly with the deliberately held SQL lock: declared5s forced137 proves crash/reclaim, not clean reference shutdown. Go and returning native exit0; cleanup0.
- [Optional selector](../qualifications/background-worker-selector-20261010/): default byte identity, only worker image changes, Recreate/default mounts preserved, six fail-closed cases; separate completed contract task.
- Static supported OpenSSL3.5.9/helper/CA/licenses and bot code/tests are unchanged from successful exact-image TLS/security/lifecycle proof; [input audit](../../../go/services/background-worker/evidence/final-original124-20261010/input-audit.json) records reuse. No native GHC/Cabal/Nix rebuild or native14 unit execution is claimed; unchanged Haskell retains upstream CI.

Implementation is complete and self-approved within the user's delegation.
Separately scoped verification/selection/service records publish legal availability
and exact compatibility. Haskell remains supported/default. No remote push,
production switch or extra speculative tests follow from qualification.

Everything below is historical construction evidence or the original execution
plan. Its outstanding-work statements describe those dated artifacts, not the
current candidate. Current decisions are above, in REVIEW.md and machine records.

## Earlier construction checkpoint (2026-10-10)

Reconciled documentation against committed implementation and receipts at
`972e97540c14d6087a2b885b6c631b260ee8edb8`. The active goal is completion of
the whole background-worker, with Haskell remaining supported and default.
The implementation task and service record remain active/implementing; no
background-worker compatibility or deployment availability is advertised.

| Evidence | What it establishes | Remaining limitation |
| --- | --- | --- |
| [Original gundeck40](../../../go/services/background-worker/evidence/jobs-original-20261009/) | 40/40 original assertions pass with Go worker and Haskell peers | Earlier executable; notification/DLQ/queue lifecycle only |
| [Original MLS migration](../../../go/services/background-worker/evidence/conversation-original-20261009/) | Five storage phases and final shared MLS history pass | One case with Haskell peers; not the final mixed-stack qualification |
| [Original users2](../../../go/services/background-worker/evidence/user-original-20261009/) | 1/2 cases pass; retained SCIM500 failure occurs before migration activation | Enabled user migration remains unverified; failure is not waived |
| [Scheduled schema](../../../go/services/background-worker/evidence/scheduled-schema-20261009/) and [reaper](../../../go/services/background-worker/evidence/scheduled-reaper-20261009/) | Native schema comparison and isolated PostgreSQL storage/gate checks pass | Connected to service startup; original whole-process qualification outstanding |
| [Adminless notifications](../../../go/services/background-worker/evidence/adminless-notifications-20261009/) and [scheduling](../../../go/services/background-worker/evidence/adminless-scheduling-20261009/) | Source-based adapters and their focused Go/storage checks pass | Connected to both pools; enabled external-peer behavior not qualified |
| [Scheduled execution](../../../go/services/background-worker/evidence/scheduled-execution-20261009/) | Go checks and isolated PostgreSQL completion, retry, lease and rollup checks pass | Earlier execution component proof; later pool/runtime receipts listed below |

Pool dispatch, worker registry heartbeat, pause/cancel notifications, a shared
reconnecting listener and transactional typed claim validation are constructed.
[Exact construction evidence](../../../go/services/background-worker/evidence/scheduled-pool-20261010/)
records passing Go checks and a real isolated PostgreSQL fixture, including claim
rollback, reconnect, registry restoration and durable finite drain. The retained
initial failure was a synthetic queue setup error. The pools are now connected to domain handlers, cron and reapers, with source
schema-before-health startup, per-pool drain and the native meetings counter.
[Combined PostgreSQL construction checks](../../../go/services/background-worker/evidence/scheduled-runtime-20261010/)
pass on guarded adminless jobs, expired meeting/code removal, manual cron,
reaper gates and shutdown. Full Cassandra/Brig/Rabbit/bot interactions and
Service.Run's complete process startup still require original-test qualification.
Pusher retry/discovery and finite-DHE bot transport are now constructed with
focused evidence. Next audit ordinary HTTP defaults/stored diagnostics, resolve
the pre-enable SCIM failure and validate the current whole process in Docker. Completion still requires the exact
final six-Go/Haskell-peer run (124 executions,120 distinct original assertions),
resolution of retained failures, bounded operations, actual durable
Haskell->Go->Haskell switchback, optional image/selector and delegated same-author
review. Reuse existing tests and successful evidence for unchanged inputs; add
validation only for concrete uncovered contract risks. Historical receipts retain
their tested revisions and do not qualify later code.

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

## Historical implementation and retained validation

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
expensive tests without a repair. Actual user migration effects remain unverified.

Scheduled payloads, Arbiter claims/heartbeat/retry/ACK/worker registration,
sibling locking/wakeup and archive retention are constructed. Native schema setup is constructed and checked;
Reaper and cron components are constructed and checked; adminless/meetings domain
handlers and scheduled runtime integration are now connected; full external-peer
qualification is unfinished.
The initial original MLS case reached enabled phase3 and reported both migration
loops completed (local32/remote2), but timed out because the native helper expects
counter text1.0 while Go emitted1. [Failed proof](../../../go/services/background-worker/evidence/conversation-original-initial-20261009/)
is retained; phase4/5 and shared-history checks did not run. Candidate counters
now match native decimal formatting and full Go checks pass. The corrected exact static executable on309733bb48febdb8708e6b1c5aadb94cb03fe8ab
then passed the unchanged original MLS case1/1 with only Go worker/Haskell peers,
no errors/skips/failures, owned cleanup0 and empty project labels.
[Passing proof](../../../go/services/background-worker/evidence/conversation-original-20261009/)
covers all five storage phases, enabled local/remote migration and final shared
MLS history. This is limited real DB/interoperability evidence, not the final
whole-service or six-Go run. Scheduled jobs and per-pool drain are now connected. Resolve the remaining
transport and diagnostic gaps before final original-test qualification. Final original47/exact six-Go124, actual durable
Haskell->Go->Haskell switchback, bounded operations, optional image/selector and
delegated same-author review remain mandatory. Native local Haskell build checks
remain unavailable/unrun. Service/task/goal remain active; Haskell stays default,
with no compatibility or availability claim.

Serious inherited concerns include PostgreSQL sslmode=prefer plaintext fallback,
transient/unconfirmed child-job publication, write-before-notification loss on
retry, poison-job requeue loops and bot pin authentication without hostname checks.
Native RSA-only bot TLS permits finite-DHE AEAD suites absent from Go TLS; providers
supporting only those suites now have an OpenSSL stream fallback under construction.
Its focused patched-CLI checks pass; final image and whole-service qualification
are still required.
Native URI IPv6 serialization removes brackets and can produce ambiguous stored
URLs. Keep these findings separate from the parity implementation; no production
security policy change is authorized. Eventual distribution must include retained
email/URI BSD3 and Arbiter MIT notices.

## Historical construction checkpoints

The following entries describe successive implementation increments and evidence
for their exact inputs. Their next-step notes are historical; the reconciled
checkpoint above and active task own current status and remaining acceptance.

Scheduled bootstrap preserves pinned native SQL bodies, registry names/order,
Base64-MD5 migration checksums, dedicated native advisory lock, native transaction
commit/rollback distinctions, default durability and adminless partial index.
[Exact construction receipts](../../../go/services/background-worker/evidence/scheduled-schema-20261009/)
include passing full Go checks and an actual isolated PostgreSQL qualifier:227
native captured column records match exactly,76 ledger entries, queued-job
preservation and checksum mismatch behavior pass, owned cleanup complete.
Initial fixture TCP-readiness race is retained and repaired; no candidate/oracle
assertion change or failure waiver. This receipt predates runtime integration.
Schema setup now runs before health in service startup; the receipt does not qualify that complete startup path.

Scheduled reaper construction now covers the five native schema-gated operations,
including bounded scans, live-lease/suspension guards, per-row worker thresholds,
group refresh locks, tree-aware DLQ snapshots and parent resume, cancelled-child
cleanup, archive expiry, transaction-local timeout and committed watermark rules.
[Exact receipts](../../../go/services/background-worker/evidence/scheduled-reaper-20261009/)
retain full passing Go checks and an actual isolated PostgreSQL test of these
storage/concurrency/rollback effects, with all owned resources removed. No fresh
Haskell reaper execution or full scheduled claim. This receipt predates runtime
integration. Reaper/schema components are now connected; their original whole-process qualification remains outstanding.

Cron persisted storage now preserves native default/override and queue-healing
semantics, five-minute manual requests, transactional claims, monotone minute/manual
watermarks and advisory leadership. [Exact construction receipts](../../../go/services/background-worker/evidence/scheduled-cron-storage-20261009/)
retain full passing Go checks and actual isolated PostgreSQL effects/rollback/lock
checks with owned cleanup. The earlier storage-only receipt remains limited to its inputs. Cron parsing,
dedup insertion and scheduler construction now pass full Go checks and actual
isolated PostgreSQL insertion/startup/wake/cancellation checks:
[exact current construction evidence](../../../go/services/background-worker/evidence/scheduled-cron-runtime-20261009/).
Native grouped payload/defaults, manual bypass/overlap, DST dedup, invalid-config
skip and nested failure watermarks are preserved. No fresh Haskell cron execution
or native package/tzdata version claim. Domain handlers and service integration/
NOTIFY remain unfinished. Next domain handlers, then the existing full-service
acceptance. Native failed insert still advances checked watermark, losing that
NoBackfill tick; record this inherited concern without changing source behavior.
Retain cron BSD2 notice alongside existing notices in eventual distribution.
No original assertion/schema/default change or broad parity claim.

Meetings SQL and cleanup orchestration are constructed with source full-row decoding,
two indexed expiry queries/stable merge/cap, strict cutoff/open recurrence rules,
canonical meeting code key and configured code-mode routing. Deletion guards/order,
full/partial batches, caught-error run count and cancellation are covered. Full Go
checks and an actual isolated PostgreSQL fixture using9 unchanged readonly native
migrations pass: [exact evidence](../../../go/services/background-worker/evidence/meetings-cleanup-storage-20261009/).
This earlier fixture stubs missing conversations; it does not prove conversation/proposal/
MLS cleanup or runtime/metrics. The later deletion construction checkpoint below
adds a separate, limited SQL proof. Native
cleanup does not send notifications through its unchecked delete path. At this checkpoint the next work was the deletion adapter and adminless handlers, then full scheduled runtime
and existing final acceptance. No fresh Haskell cleanup/whole-service proof.

Meetings configuration/cutoff is now connected to cleanup orchestration. Required
numeric/text settings and canonical source CronSchedule serialization are validated;
Double-to-Pico before3600 multiplication, SQL microsecond floor and native Int64
wire conversion avoid Go duration overflow while preserving source behavior.
[Exact full Go checks and focused evidence](../../../go/services/background-worker/evidence/meetings-config-20261009/)
pass; a miscalculated candidate test date was corrected and its failure retained.
No fresh native execution/package-version or new DB claim; reuse unchanged prior
storage evidence. Serious inherited concern: enormous finite retention may wrap
native timestamp encoding to a recent cutoff and expire unintended meetings. Keep
parity now; any new bound/encoding policy needs separate review. Next actual deletion
adapters/adminless and scheduled runtime, then unchanged full final acceptance.

Unchecked conversation deletion is now constructed, retaining source reread,
sorted subs, MLS/current-history-proposal order, code revocation and team/nonteam
conversation removal without notifications. Source per-operation migration locks,
pending cleanup, existence routing and Cassandra-first dual writes are preserved;
history stays PG-owned and proposals Cassandra-owned. Full Go checks and actual
isolated PostgreSQL deletion using four unchanged native schemas pass:
[exact construction evidence](../../../go/services/background-worker/evidence/conversation-removal-20261009/).
The SQL qualifier explicitly stubs conversation reads and Cassandra proposals;
no cross-store/full-service/native execution claim. Same-author construction
review uses delegated self-approval, not independent review. Next adminless
handlers and whole scheduled runtime/NOTIFY/metrics/startup/drain; unchanged final
original47/six-Go124, unresolved users2 SCIM500, operations/durable switchback,
optional image/selector and whole-service review remain mandatory. Goal/task
active, Haskell supported/default, no candidate availability or compatibility.

Adminless setup/deletion/reminder domain decisions and concrete team scans/local
promotion are constructed with source guards, Brig eligibility, original selection
vectors, native role-write/notification order and per-job remote capability cache.
Direct system deletion checks eight backends concurrently and treats federation
failures as unsupported; user deletion/reminders retain their distinct gates.
[Exact construction evidence](../../../go/services/background-worker/evidence/adminless-core-20261009/)
retains full Go checks and actual native-schema PostgreSQL team/promotion/deletion
proof. Pure original selection vectors are reused in candidate tests, not freshly
executed Haskell tests. Real feature/account/notification/scheduling adapters and
scheduled service integration remain unfinished. Inherited role-write-before-
notification retry can skip remaining notifications/promotions after an error;
preserve source behavior and record a separate outbox/transaction improvement.
Next complete those adapters and runtime, then unchanged whole-service acceptance.

Adminless feature defaults and concrete storage lookup are implemented, preserving
server lock/defaults, partial DB patches, ignored stored legacy-day fields, duration
precedence/fixed-Pico arithmetic and native Word64-day bounds. Migration reads under
the source500ms shared team/feature lock, prefers any PG row, then CQL fallback.
[Exact construction evidence](../../../go/services/background-worker/evidence/adminless-features-20261009/)
retains full Go checks and original-schema PG defaults/patch/lock/type/NULL proof;
actual Cassandra/fallback/native execution and whole-service runtime are unproven.
Attoparsec BSD3 source notice must join other notices in eventual distribution.
Next account/notification/scheduling adapters. Native malformed full Brig User
response becomes an empty chunk; this can trigger the no-eligible cleanup branch.
Preserve full decoder behavior and record this inherited concern. Only this Galley
feature is validated here; no full unrelated configuration claim. Final acceptance
remains unchanged and incomplete.

Adminless Brig account decoding and HTTP lookup are constructed. Five unchanged
original User goldens and focused complete-row/null/type/retry/chunk/header tests
pass full Go checks: [exact construction evidence](../../../go/services/background-worker/evidence/adminless-accounts-20261009/).
Readonly metadata identifies the pinned native worker timestamp parser as
attoparsec-iso8601 1.1.1.0; superseded 1.1.0 comparison is retained separately.
No fresh Haskell decoder or live Brig interoperability claim. Source malformed
successful response becomes an empty chunk; unbounded body and consequent
no-eligible deletion risks are recorded without changing policy. Runtime transport
defaults still require integration. Setup scheduling preserves the scanned team
ID separately from the conversation team used for feature lookup. Next notifications,
durable scheduling and scheduled runtime/NOTIFY/metrics/startup/drain, then the
unchanged full-service acceptance. Haskell remains supported/default and this
whole-service goal remains active and unqualified.

Adminless durable deletion/reminder insertion is constructed with source single-Now,
strictly earlier reminders, insertion order, independent commits, grouped payloads,
IgnoreDuplicate and default fields. Fixed Pico arithmetic preserves unwrapped
millisecond JSON separately from PostgreSQL microsecond/Int64 encoding. Full Go
checks and actual isolated native-schema PostgreSQL durability/dedup/group tests
pass with cleanup: [exact construction evidence](../../../go/services/background-worker/evidence/adminless-scheduling-20261009/).
No fresh native scheduling execution or full runtime/switchback claim. The existing
reminder decoder/notification time.Time representation must carry extended accepted
timestamps before qualification. Next notification adapters and scheduled service
integration; existing final acceptance remains unchanged and incomplete.

Adminless promotion/deletion/reminder notifications are constructed, preserving
system/user event origins, V4 snake_case versus action-update camelCase bundles,
version/policy, recipient/bot/Cells distinctions and remote-before-local order.
Capability lookup preserves native negotiation plus explicit API-version call,
proved with the existing synthetic h2c transport fixture. Scheduled reminders now
carry extended/signed/offset/leap timestamps through execution and notification
as exact text; unchanged original reminder golden reused. Native %Y is unpadded.
Earlier Go federation response caps were removed to match full native response
acceptance; one above8MiB fixture covers the repair and inherited memory risk is
recorded. [Exact full Go/source-review evidence](../../../go/services/background-worker/evidence/adminless-notifications-20261009/)
passes; no fresh native or whole-service E2E proof. Retain time BSD notice for
distribution. Next scheduled runtime/dependency wiring/NOTIFY/metrics/startup/drain,
then unchanged final qualification, pusher timing and botTLS gaps included.

Scheduled SingleJobMode execution is constructed with held control transaction,
independent handler effects, unit/no-result completion, exact ACK and separate
retry/DLQ, lease/reclaim/cancel/shutdown decisions and native fixed-Pico jitter/
ceiling. Full Go checks and actual isolated native-schema PG control/independent
effects/archive/stale-guard/cancel/rollup proof pass with owned cleanup:
[exact construction evidence](../../../go/services/background-worker/evidence/scheduled-execution-20261009/).
Helper identity predates only an executor jitter correction and its storage
qualifier never calls that executor; final current code has separate passing
checks. No full scheduled runtime/native comparison. Native rollup descendant
cascade precedes guarded root move, an inherited ownership-race concern recorded
without silently changing behavior. Stored error diagnostic equivalence remains
for whole-service review. Subsequent pool and runtime receipts below cover
dispatcher/registry/pause/NOTIFY and domain startup/drain/metrics construction.

Scheduled pool construction preserves native capacity, pause/resume and queued-job
handling, registry proof-of-work cadence, missing-row registration, owned typed
NOTIFY controls and one shared reconnecting listener with sticky readiness.
Claim payload decoding now occurs inside the transaction before commit, so any
malformed row rolls back the whole typed batch. Native re-registration defers
pause reconciliation until the next heartbeat. Full default Go checks and actual
native-schema PostgreSQL construction tests pass:
[exact evidence](../../../go/services/background-worker/evidence/scheduled-pool-20261010/).
The fixture covers the combined cancellation signals, not which won, and is no
fresh Haskell or full-runtime comparison. Subsequent runtime receipts below cover
domain wiring, cron/reaper, startup/drain/metrics construction; full acceptance
remains unchanged and incomplete.

Scheduled runtime integration now binds both pools to existing adminless and meetings
adapters, per-job Brig/capability request scopes, independent Rabbit publisher,
shared listener, cron and per-pool gated reapers. Source schema bootstrap runs
before health, support threads survive both drains, native cleanup run counter is
exposed, and grace expiry does not force nonzero process exit. Full current Go
checks and combined original-schema PostgreSQL integration pass:
[exact evidence](../../../go/services/background-worker/evidence/scheduled-runtime-20261010/).
The fixture explicitly has PostgreSQL-only routing, disabled adminless feature,
no external peers and bypasses full prepare/startup; no whole-service E2E claim.
Initial missing-migration/invalid-expectation/wrong-code-key fixture failures and
superseded helper identities are retained. Next source pusher discovery and botTLS/
ordinary transport/diagnostic review, then current original47/final six-Go124 and
all unchanged operations/switchback/image/selector/review acceptance.

Pusher retry/discovery gaps are repaired using source retry0.9.3.1 semantics:
saturated half-plus-inclusive-draw jitter, cap after jitter, and whole-pagination
restart with60s accumulated-wait budget. Native queue-page required exact-case
fields, integral signed metadata, page order and full body acceptance are preserved.
[Exact Go/source/HTTP evidence](../../../go/services/background-worker/evidence/notification-retry-20261010/)
passes; no fresh Haskell policy execution or current whole-service qualification.
Removing the artificial discovery response cap preserves an inherited memory/
availability concern; new limits need separate review. BSD3 notice retained.
Next bot finite-DHE compatibility, ordinary transport and stored diagnostics,
then current original tests and all unchanged final acceptance.

The owned construction-cache cleanup recovered 1,312,280,776 bytes without removing
qualified executables or reference artifacts; available space rose above the
harness 2 GiB preparation floor. The [preparation receipt](../../../go/services/background-worker/evidence/harness-preparation-20261010/README.md)
records this environment observation, not a test pass. Read-only inspection of the
pinned runner found OpenSSL 3.6.2 at its Nix store path; no fallback transport or
new production dependency is selected or qualified by that discovery.

Bot finite-DHE compatibility is now constructed with a narrow fallback from Go's
remote cipher-negotiation failure to an authenticated OpenSSL TLS1.2 stream. The
same mandatory RSA pin/signature/date verifier runs before HTTP data, and Go pin
failures cannot trigger fallback. Helpers use fixed arguments, no inherited backend
credentials, bounded transcript/time/concurrency and close/cancellation reaping.
[Exact construction evidence](../../../go/services/background-worker/evidence/bot-dhe-20261010/)
records full current Go checks and a real finite-DHE-only fixture on patched
OpenSSL3.5.9, built inside Docker from publisher-checksummed source. The older
runner CLI is fixture-only; final image must package the patched supported CLI
and its license. No Go module or Haskell dependency changed. Ordinary HTTP manager
and stored diagnostics review, SCIM repair, final original suites and all unchanged
operations/switchback/image/selector/review requirements remain outstanding.

Ordinary internal HTTP timeout phases now follow the identified http-client0.7.19
source:30s connection acquisition, then remaining-budget headers, with body reads
remaining caller-cancellable and without a new total deadline. Handler diagnostics
now flow into native guarded retry/DLQ SQL instead of a constant last_error.
[Exact construction receipts](../../../go/services/background-worker/evidence/http-diagnostics-20261010/)
record full Go checks, finite real HTTP tests and the reused PostgreSQL fixture
with nonconstant persisted diagnostics and successful owned cleanup. These are
component checks, not the final E2E or switchback proof. Native unknown exception
rendering can persist sensitive details; Go domain adapters keep errors generic
and panic values unformatted. Environment proxy semantics and reused-connection/
header-parser behavior remain in the ordinary-manager audit. Next execute the
current whole-worker original47 to expose remaining real startup/domain/storage
failures, preserving the retained pre-enable SCIM500 as an unresolved failure.

The current clean12107801e whole-worker original47 run finished44/47 with no
errors/skips and successful owned cleanup. All23 adminless, meetings and the other
non-user cases pass; three user cases fail with native CQL/Elasticsearch/Postgres
connection closures. [Exact full-run receipts](../../../go/services/background-worker/evidence/current-original47-20261010/)
retain failures and distinguish actual suite execution from the outer controller's
false E2E flag on failure. FD limit1024 is observed; a scaled original proxy probe
reproduces caught EMFILE/EOF. Actual run high-water was not captured, so attribution
is unproven. Next correct the known fixture descriptor/selector limitation under a
separate harness task and monitor a measured rerun without changing original tests.
This does not supersede unresolved old failures or qualify the complete service.

The [matched original user comparison](../../../go/services/background-worker/evidence/user-reference-pair-20261010/)
passes2/2 first with all Haskell, then with unchanged12107801e Go worker/Haskell
peers, including all five user phases and reindexing. Same runner/fixtures,
strict XML and successful owned cleanup. Wider47 SCIM500 does not reproduce in
this pair; retained failed runs are not waived. Enabled user migration now has
actual original-test evidence for that executable, not just component checks.

[Ordinary proxy/reused-connection construction](../../../go/services/background-worker/evidence/http-proxy-replay-20261010/)
now preserves environment proxy snapshot/domain rules, credential-free startup
validation and reused incomplete-header recovery. Fresh/status/one-shot/body/
cancellation negatives pass. Full Go checks pass; remaining ordinary parser audit
is explicit. Final mixed-stack124, operations, durable switchback and patched
OpenSSL image/selector are still required before whole-service qualification.

The [finite ordinary-manager source review](../../../go/services/background-worker/evidence/http-proxy-replay-20261010/ordinary-manager-review.md)
closes the ordinary HTTP audit for used valid Brig/Gundeck bindings. Native
malformed-line tolerance and fragmentation-dependent helper limits versus Go's
standard bounded parser are explicit, not a claim of arbitrary malformed HTTP or
whole-library byte identity. The frozen original assertions/service gates remain
unchanged. Next run the exact current worker with five qualified Go peers through
all original124 assertions; no speculative helper identity suite is added.

The [current exact six-Go result](../../../go/services/background-worker/evidence/six-go-original124-20261010/)
passes124/124 original executions including all47 worker cases on clean89f24e7a,
with matching selected hashes, no errors/skips/failures, controller/runner/cleanup0
and no owned resources. Actual XML/catalogues contain120 distinct functions (four
Events overlaps); previous119/five-overlaps metadata was wrong. This corrects
counting without changing any assertion; protected reference count correction is
reserved for separately scoped selector/publication work.

[Optional image construction](../../../go/services/background-worker/evidence/image-construction-20261010/)
packages the same worker with static patched OpenSSL3.5.9, qualified source CA,
UID65534 and complete helper/runtime license notices. Exact-image bot tests pass
including DHE/pin/date/cancellation/transcript/reaping. No host dependencies are
installed. The committed generic packager runs successfully and reproduces the exact tested
image. Default mounts/health and declared operations/loaded SIGTERM now pass as
recorded below; durable Haskell-Go-Haskell switchback remains outstanding.

The next operations check is declared before execution in
`go/services/background-worker/qualification/`:100 health requests, concurrency8,
plus five nonexistent-group and five nonexistent-group/channel jobs, all ACKed
with received/started/succeeded counters and inflight0 within30s. Record health
latencies and actual image process RSS/CPU. A real private h2c notification sink
holds one delivery with another pending; SIGTERM must exit0 within configured30s
plus5s and return both unsent messages to the durable queue. The exact image uses
both default config mounts, UID65534, verified Rabbit CA, no host ports, read-only
source/rootfs and owned mandatory cleanup. The qualification sink is separate
from the shipping executable/image. This is operations evidence, not another
original-suite run or the required complete durable switchback.

The declared operations smoke passes on the exact qualified image:100 health
requests/concurrency8 and ten jobs in0.610s, correct counters/ACK/busy0, RSS/CPU/
latency samples retained. Actual blocked h2c delivery has one unacked/one ready;
SIGTERM exits0 in0.0595s and returns both unsent messages to ready. Both default
mounts, UID65534 and verified broker CA work; cleanup0/no owned resources.
[Exact result and finite limits](../../../go/services/background-worker/evidence/operations-image-20261010/)
are retained. Remaining durable switchback/selector/final review gates are unchanged.

The durable rehearsal is declared in `qualification/run_switchback.py` before
execution. One private Rabbit/PG/Cassandra namespace keeps all state while actual
native Haskell, the exact89f24e7a Go image, and native Haskell run sequentially.
Use supported synthetic2s scheduled grace/5s visibility/1s job heartbeat and a
far-future cron to observe manual jobs predictably; operations30s remains separately
qualified. Hold an expired meeting row to observe native claim/attempt1 and later
Go reclamation/new owner/increased attempt, then release and verify archive,
meeting/code cleanup and delayed conversation-job completion. Queue missing-group
jobs and actual notifications at handovers, observe unsent ready/unacked state,
then successful Haskell delivery/ACK with exact synthetic payloads. Seed an explicit
previous-partial user copy (PG row/pending marker plus Cassandra source), verify Go
continuation preserves that row and clears source/marker; on return Haskell copies
a further Cassandra user and preserves Go's durable state. The partial fixture is
not claimed to have been generated by Haskell. Native original DDL and real
workers perform all continuation/effects; snapshots/identities and mandatory owned
cleanup are retained. No simultaneous workload owners, oracle/schema changes,
production endpoint, push, or extra original suite run.

The initial durable rehearsal captures native attempt1/owner/lease and queued
state, but native SIGTERM does not finish within35s with the synthetic meeting
row indefinitely locked. No Go owner started; failed receipt and successful
cleanup are retained in `evidence/switchback-initial-20261010`. Exact causal
isolation is not claimed. The subsequent rehearsal predeclares initial native
container termination with5s allowance, accepting/recording SIGKILL137 only for
that initial reference owner, then proving it is stopped and the same lease is
reclaimed by Go. This exercises actual crash/reclaim switchback as an orchestrator
would bound a blocked owner. Go's clean shutdown criteria are unchanged and already
pass the separate30s-grace loaded smoke; middle Go and final Haskell stops must
exit0. No candidate failure, protected test or original acceptance is waived.

The bounded rehearsal reaches Go lease reclamation and completion, partial user
continuation and clean loaded stop, then return-Haskell storage convergence.
Notification ACK fails because the qualification sink returned200 with no JSON
media type; actual Haskell reports UnsupportedContentType application/octet-stream
and retries. Retained `evidence/switchback-media-20261010` includes the failed run
and cleanup0. This is a fixture defect and exposes a concrete candidate ACK/retry
gap: the current Go handler accepts arbitrary2xx response media/body. Primary
`Wire.API.Federation.BackendNotifications` declares Post200 '[JSON] EmptyResponse;
`API.Common` accepts any JSON object, ignoring its fields. Correct used-binding
success acceptance (200, application/json, object for notification response), keep
secret-free retry errors and full body semantics, and cover valid/invalid media,
status/body plus retry-before-ACK. Set valid fixture success to200/JSON/{}. This is
not a parser identity expansion or changed reference expectation. The changed
production executable requires current Go checks and exact final mixed124/image/
operations/switchback evidence; old89f24e7a receipts remain historical qualification
of that binary and cannot approve the revised one.

[Current exact durable switchback](../../../go/services/background-worker/evidence/switchback-final-20261010/)
passes all three actual owners on the same Rabbit/PG/Cassandra state. Native
claim1 survives its explicitly bounded forced stop; Go reclaims/archives attempt2
under a new owner, completes scheduled work and partial user cleanup preserving
the target row. Return Haskell migrates a further user, completes another meeting
job, preserves shared archives/data and ACKs both actual notification bodies.
Go and return Haskell SIGTERM exit0; cleanup0/no owned resources. Required current
original124/operations and protected selector/final review gates remain pending.

[Current exact-image operations](../../../go/services/background-worker/evidence/operations-final-20261010/)
passes the declared100health/8concurrency and10job smoke in0.597s, measured RSS/CPU/
latency and clean30s-grace loaded SIGTERM0/.0577s preserving both unsent messages;
cleanup0/no owned resources. Separately scoped
[optional selector/count review](../qualifications/background-worker-selector-20261010/)
passes actual default byte identity, one-image replacement and six fail-closed
CLI cases; Recreate/mounts/probes remain unchanged. Primary contract count is
corrected120/four Events overlaps without changing required124 assertions.
Current original124 and final delegated same-author review/publication remain.
