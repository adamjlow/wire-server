# Background-worker source inventory

Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Haskell remains supported/default.
Inventory is source evidence, not a Go compatibility or completed-test claim.
The [plan](../../plans/background-worker.md) preserves the whole service.

## Process, configuration and dependencies

[exec/Main.hs](../../../../services/background-worker/exec/Main.hs) reads both
background-worker and galley YAML, accepts -c/--config-file and
--galley-config-file, defaults to the chart's /etc/wire paths. Published Haskell
help matches these flags; actual executable SHA 985aab36... in test-discovery.json.
[Options.hs](../../../../services/background-worker/src/Wire/BackgroundWorker/Options.hs)
requires endpoint/worker/migration/database configuration. Background jobs have
concurrency and maxAttempts 1..1000 plus per-attempt Duration. Scheduled jobs default
poll 5s, threads 1, visibility 60s, job heartbeat 30s, worker heartbeat 10s, backoff 2/cap 1d,
equal jitter, graceful timeout 30s, reaper 5min/timeout 5min/stale 5min. Positive-duration
validation, positive cleanup batch, nonnegative retention and valid cron required.
Rabbit config supports AMQP-only or admin+AMQP forms; admin capability controls
federation queue discovery. Preserve source's behavior/readiness when absent.

[Env.hs](../../../../services/background-worker/src/Wire/BackgroundWorker/Env.hs)
connects Cassandra gundeck/galley/brig, PostgreSQL pool/secret Arbiter connection,
RabbitMQ publisher channels, HTTP/HTTP2 managers, federation domain and galley
conversation/security/MLS/rate-limit settings. Brig/galley/gundeck/spar/federator
remain network peers, not embedded runtimes. Existing schema/storage-location
options choose cassandra/postgresql/migration-to-postgresql. Trace
[Effects.hs](../../../../services/background-worker/src/Wire/Effects.hs) interpreters
when implementing jobs; do not delegate new worker logic to a Haskell shim.

## Worker contracts and persisted effects

| Concern | Source boundary | Observable behavior to freeze |
| --- | --- | --- |
| Federation pusher | Wire/BackendNotificationPusher.hs | Admin API discovers paginated backend-notifications.<domain> queues; validated domains, polling, quorum/single-active queue declaration and channel prefetch 1, preserving per-target ordering. Legacy notification or versioned nonempty bundle; query Brig api-version over federator, latest compatible payload/tie handling, KeepQueued vs DropIfUnsupported, body/component/origin/request/version propagation. Successful send/ignored version mismatch ACK; remote inner bad-request rejects without requeue; malformed payload rejects; other failures jitter-retry. Recovery after queue deletion/channel loss, drain in-flight work. |
| Dead-user watcher | Wire/DeadUserNotificationWatcher.hs | Quorum user-notification DLQ; transient delivery-mode 1 ACK; persistent x-last-death-queue identifies user-notifications.<UUID>.<ClientId>, writes Cassandra missed_notifications then ACK. Invalid/missing headers throw; reconnect/readiness semantics retained. |
| Rabbit background jobs | Wire/BackgroundWorker/Jobs/Consumer.hs + libs/wire-api/Wire/API/BackgroundJobs.hs | Durable nonexclusive background-jobs quorum queue, prefetch=config concurrency; envelope id/requestId/payload. Invalid JSON waits 200ms then rejects/requeues; valid job repeats bounded timed attempts with jitter 100ms base, ACK success or rejects without requeue after exhaustion. Metrics label job type and redelivery/in-flight/duration. |
| Group/channel synchronization | libs/wire-subsystems/Wire/BackgroundJobsRunner/Interpreter.hs | Two payload tags sync-user-group and sync-user-group-and-channel. First enumerates group channels and republishes per-channel jobs; second finds group/conversation, adds missing local users up to hard member limit with wire_member role, persists compatible members and emits original local/remote/bot join events. Missing group/channel and empty/new-member cases follow source. |
| PostgreSQL scheduled workers | Wire/BackgroundWorker/Workers.hs + libs/wire-api/Wire/API/Jobs.hs | Separate meetings/conversations Arbiter queues/tables and worker pools; transactional claim, heartbeats, visibility/reaper/retry, grouped job scheduling/cancellation. Cron meetings-cleanup uses SkipOverlap, maxAttempts 3. Conversations jobs are adminless setup/deletion/reminder. Payload encoding is protected existing API golden material. |
| Adminless jobs | Wire/AdminlessJobsWorker.hs + libs/wire-subsystems/Wire/ConversationSubsystem/AdminlessGroups.hs | Setup reevaluates eligible conversations/feature/group/member state and schedules follow-ups; deletion verifies current eligibility then removes conversation and emits local/federated side effects; reminder sends deletion timestamp. Orig user can be absent/system; team scope/cancellation/idempotency and cross-runtime queued jobs must remain compatible. |
| Meetings cleanup | Wire/MeetingsCleanupWorker.hs + libs/wire-subsystems/Wire/MeetingsSubsystem/Interpreter.hs | Cutoff=now-retention, recurrence-aware batches, source deletion/conversation/notification effects; repeat full batches, stop on partial or subsystem error, cleanup runs counter, zero/negative config validation. |
| Seven opt-in migration families | Wire/PostgresMigrations.hs + libs/wire-subsystems/Wire/*Store/Migration.hs | Conversation + remote user memberships, codes including TTL/password/targets, team feature rows, domain registration, users/handles/deletions/reindexing, block-list, password-reset. Page/concurrency/attempt-timeout, advisory migration locks, retry row errors, resume/checkpoint and final metrics; external terminal error marks failed and requires process restart. Cassandra reads/Postgres writes must match source exactly, with no new Go schema. |

Arbiter external source is pinned in flake.lock to velveteer/arbiter
b9c57eb1f8277d97616aa449bea471fe9ce14eda; integration must inspect actual DDL/queue
and lease format, not invent a parallel Go queue. Galley owns upstream schema
migrations; worker owns Arbiter job setup and opt-in data migration loops.

## Operations, deployment and security

[BackgroundWorker.hs](../../../../services/background-worker/src/Wire/BackgroundWorker.hs)
starts all workers plus only enabled migrations, runs HTTP health, and invokes
parallel cleanup actions under source server signal handling. Health /i/status is
NoContent; /i/status/workers fails 500 with named stopped workers in ordered map.
[Health.hs](../../../../services/background-worker/src/Wire/BackgroundWorker/Health.hs)
and Env worker gauge/counters name pusher/watcher/jobs; scheduled job metrics and
migration outcome metrics must be inventoried in the frozen operational matrix.
Runtime/diagnostic values are not literal cross-language equality requirements.

[chart deployment](../../../../charts/wire-server/templates/background-worker/deployment.yaml)
uses Recreate to keep one worker version running, mounts both configs/secrets and
probes /i/status. Selection must change only its image and preserve Recreate,
existing defaults, mounts and graceful lifecycle. No exec preStop hook found.

Serious inherited concerns: malformed Rabbit job JSON requeues indefinitely;
malformed federation notifications are dropped (upgrade/data-loss risk), KeepQueued
can block a target indefinitely, domain metrics are unbounded, debug logs contain
job/domain metadata, invalid DLQ headers can trigger reconnect/redelivery loops.
Record these, preserve contract, no silent privilege/retry changes. Validate domains,
job payloads and queue headers, use parameterized SQL, verified TLS and bounded
resource/concurrency controls; never disclose secret connection strings or execute
job input. Cross-service job interpreters exercise auth/member/business state,
not merely database row deletion; scope cannot shrink to a notification-only worker.

## Existing test reuse and concrete gaps

[test-discovery.json](test-discovery.json) records actual nonroot network-none
compiled --list/help discovery: 1001 listed entries, 47 relevant candidate functions
(23 AdminlessGroups,15 Migration,3 UserGroup,1 Meetings,1 Federation,4 Events).
Discovery executes no tests. background-worker-test is absent; native unit tests
in services/background-worker/test remain source-only/unrun without GHC/Cabal/Nix.
Original source includes per-family migration behavior and scheduled/adminless job
state/notifications; use these assertions rather than translating them to Go.
The prior [patched original 77](../../qualifications/go-patched-20261009/README.md)
ran Haskell background-worker alongside five Go peers and already passed existing
Events/dead-letter cases; that is reusable Haskell behavior, not Go-worker parity.

Contract task must run relevant new original functions against Haskell and freeze
only uncovered direct boundaries: payload/version/ack/drop/retry, malformed job/DLQ
headers, worker readiness failure/reconnect, Arbiter lease/serialized job handover,
migration resume/lock/timeout effects, one operations smoke and loaded shutdown.
A six-Go selector/catalogue extension is required before Go-worker original tests;
current strict controller deliberately accepts only the earlier five candidates.
Pending exact schema/capture choices are contract work, not missing source scope.
