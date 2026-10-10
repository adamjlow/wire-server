# Background-worker finite replacement contract

Status: reference contract frozen after source/capture/coverage review. This is
not candidate qualification or a passing whole-suite baseline. Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e;
actual published executables and fixture identities are in oracle receipts.
Haskell remains supported/default. This is a whole-service boundary.

## Acceptance and reuse

Use the original 47 functions in
[the strict catalogue](../../../../tools/parity/docker/background-worker-tests.json),
not translated request specifications. They cover 23 adminless cases, 15 migration
cases, three group synchronization cases, meeting cleanup, offline federation and
four notification/recovery cases. Original assertions remain unchanged.
The compiled background-worker-test executable is absent; its 14 source tests in
[BackendNotificationPusherSpec.hs](../../../../services/background-worker/test/Test/Wire/BackendNotificationPusherSpec.hs)
remain unexecuted. Preserve their actual expected bundle/version/ACK/reject/retry
rules as source-owned oracle material rather than claiming that suite passed.

The first five-Go/Haskell-worker run passed 45/47: DNS fixture missing, MLS
creation returned 533 before migration. The corrected all-Haskell run passed 45/47:
DNS and MLS passed, two user tests failed with setup/search 500 and Cassandra
errors; both passed in the initial mixed run. See the retained failed XML,
receipts and summaries. None is a passing whole-suite baseline. The corrected mixed run passed 46/47, with only MLS creation returning 533
again. Targeted original profiles now isolate the MLS case and the two user
cases; failed reports remain retained. Original isolated five-Go MLS then passed
twice with an unchanged categorized diagnostic executable. Every required case
has a passing observation across identified runs (original-coverage.json); this
does not establish one clean whole-suite baseline. The intermittent transport
issue remains open, federator compatibility null, temporary diagnostics removed.
Freeze source behavior independently and build the worker; final exact six-Go
original124 tests must all pass. Never skip a case or widen an expected status.

Final original selection is six Go services and remaining Haskell peers. The
combined profile runs existing 77 plus worker 47 assertions (124 executions,
120 distinct functions because four notification cases overlap). Every selected
binary hash, full build revision, runner/dependency identity, XML, process status
and cleanup status is retained. Earlier unchanged service quality evidence is
reused; no full Haskell rebuild is claimed locally.

## Contract boundaries

| Boundary | Required behavior and existing evidence |
| --- | --- |
| Configuration/startup | Both worker and galley YAML/JSON; -c/--config-file and --galley-config-file plus existing /etc/wire defaults. Three Cassandra clients, galley PostgreSQL pool/password, verified Rabbit TLS/admin and internal HTTP2. Preserve all seven opt-in migration flags, each storage-location mode and bounded validated settings. AMQP-only admin absence skips discovery and leaves its readiness status consistent with source. |
| HTTP/metrics | Actual Haskell /i/status and /i/status/workers both 200/empty when healthy; /i/metrics 200. Failed worker readiness 500 with exact ordered names and source text. Keep worker gauges, notification pushed/dropped/errors/stuck labels, background job concurrency/busy/received/redelivery/invalid/attempt-duration, meetings and per-family migration counters. Runtime/GC/pool diagnostics describe their own runtime; values are not literal cross-language equality requirements. |
| Federation delivery | Paginated admin discovery; valid backend-notifications.<domain>, source queue/quorum/single-active declaration and prefetch 1. Legacy payload fallbackV0, default request ID, bundle policy defaultKeepQueued, latest common version, original raw body/path/origin/component propagation. ACK successful/version-negotiation-ignored delivery; reject malformed/inner400 bad-request without requeue; retry other failures with configured jitter; KeepQueued remains unacked, DropIfUnsupported ACKs. Original offline/recovery cases plus source 14 test expectations protect version/retry boundaries. |
| Dead-user watcher | Source DLQ/quorum declaration, transient mode 1 ACK without storage write; persistent x-last-death-queue parses user-notifications.UUID.ClientId, parameterized LocalQuorum missed_notifications insertion then ACK. Missing/malformed header throws without ACK; channel/connection reconnect/readiness follows source. Original Events exercise transient/persistent storage and reconnect. |
| Rabbit jobs | Durable nonexclusive background-jobs quorum queue, configured prefetch/concurrency, id/requestId/payload envelope. Two tags: sync-user-group and sync-user-group-and-channel. Invalid JSON delays 200ms/requeues indefinitely; successful dispatchACK, per-attempt timeout and finite jitter retries, exhaustion reject/no requeue. Preserve counters and in-flight gauge. UserGroup original 3 protect async membership effects. |
| Group sync | Enumerate group channels and republish with fresh UUID/propagated request ID; add missing local group users only, hard member limit and wire_member role, compatible local/remote/bot join notifications. Missing group/conversation and no-new-user cases follow source. Existing interpreter, stores and notification functions define persistence/serialization, not an invented Go API. |
| Scheduled jobs | arbiter schema, separate meetings/conversations tables and pools, original JSON type/data envelopes and tags meetings_cleanup/adminless_setup/adminless_deletion/adminless_reminder. Preserve grouped queue heads, priority/order, transactional SKIP LOCKED claim, incremented attempts/visibility/claimed_by, optimistic attempt guard for heartbeat/ACK/retry, cancellation, stale-worker/reaper and configured jitter. Existing DDL/triggers/migration state is shared with Haskell. |
| Adminless work | Actual implementation is ConversationSubsystem/Update.hs (setupAdminlessGroupsCleanup, adminlessTryAutopromote, adminlessAutopromoteOrDelete/SendReminder); AdminlessGroups.hs supplies candidate selection. Reevaluate current feature/group/team/member state, promote eligible members by configured strategy, otherwise schedule/delete/remind. Optional origin changes user/system events; remote system-delete version support may suppress deletion. Preserve notification/store effects and team-scoped cancellation. Original 23 cover these branches. |
| Meetings | MeetingsStore/Postgres.hs selects nonrecurring end_time and recurring effectiveEndTime separately, merges/reorders/caps batch. Force deletion removes a matching MeetingConversation, meeting code and meeting row with original local/federated effects. Repeat full batches, stop on partial or subsystem error; cron SkipOverlap and maxAttempts 3. Original cleanup verifies effects. |
| Seven migration families | conversations plus remote memberships, codes/expiry/password/targets, team features, domain registration, users/handles/invalid/deleted/reindexing, block list, password reset. Original 15 use Cassandra -> migration-to-postgresql -> postgresql phases and restart worker, preserving locks, pagination, concurrency, attempt timeout, row-error retry/completion counters and storage continuity. Preserve all source schemas and supported modes; no Go-only tables or silent no-op migration flags. |
| Shutdown/selection | Preserve signal handling/cleanup and Recreate deployment strategy, both config mounts, existing probes/defaults. Operator chooses only the service image. Never run two implementations as owners of one workload during switchback. |

Arbiter is pinned to b9c57eb1f8277d97616aa449bea471fe9ce14eda. Read-only
actual PostgreSQL capture contains 227 columns across source tables; its DDL,
columns and worker/cron rows are retained in oracle/. Claim, lifecycle, worker,
cron and migration source hashes are in arbiter-source-identities.json. Jobs have
attempt checks, grouped admission and source-maintained trigger state; a plain
SELECT/DELETE loop would violate this contract. Preserve source job initialization
and migration ledger, not just existing-table operation.

## Only concrete supplementary gaps

Add focused candidate tests for source 14 pusher expectations (legacy, policy,
latest common version, malformed payload, bad-request drop and bounded retry),
configuration validation, malformed job/DLQ handling, readiness ordering and
Arbiter lease/attempt guards. Reuse original assertions for business/storage paths.
No exhaustive field-by-field or runtime diagnostic identity suite.

One operations smoke, defined before measurement: 100 health requests at concurrency 8
plus 10 synthetic background jobs with no matching group/conversation; no failed
health response, all expected ACKs/counters and no remaining in-flight jobs within 30s.
Record RSS/CPU/latency, do not impose invented Haskell-vs-Go runtime values. SIGTERM
must terminate the entire process within configured 30s grace plus 5s allowance,
including consumers and worker pools. Required loaded notification shutdown must
also preserve pending/in-flight delivery and avoid acknowledging unsent messages.

One real Haskell->Go->Haskell rehearsal uses the same owned Rabbit/Postgres/Cassandra
state with queued Rabbit jobs/notifications and scheduled conversation/meeting jobs,
including one in-flight lease and a pending migration continuation. Stop each owner
before starting the next; confirm ACK/redelivery, payload decoding, attempt/lease
state, data/notification effects and return to Haskell. Record exact binaries and
state snapshots. Source-shaped mocks alone cannot qualify this rehearsal.

Security: validate domains, tagged payloads and queue headers; verified TLS,
parameterized queries, bounded resources/concurrency, generic secret-free errors.
Record inherited poison-job infinite requeue, malformed notification drop,
KeepQueued target blockage, malformed DLQ callback/reconnect loops, unbounded domain
metric labels and group-sync legalhold/MLS gap; preserve behavior and do not grant
unreviewed privilege/security exceptions. All fixtures synthetic/private, no host
ports or implicit networks; owned cleanup mandatory. GHC/Cabal/Nix unavailable.
