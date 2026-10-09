Partial job construction checks

Pinned Go1.26.9 full module checks and race tests passed against the module source
identified in source-files.json. Checks ran before the construction commit; no
clean final binary, actual job delivery or whole-service qualification is claimed.

Source boundaries: Wire.API.BackgroundJobs, BackgroundWorker.Jobs.Consumer,
BackgroundJobsRunner.Interpreter and BackgroundJobsPublisher.RabbitMQ. Tests
cover mandatory/tag/UUID fields, unknown fields and optional actors, source retry
exhaustion/timeouts and ACK/requeue rules, context cancellation without ACK,
member limit including bots, user/SCIM actor and recipient propagation, missing
group/conversation behavior and writes before notifications. PostgreSQL group
queries are bound parameters; password-file override, injection-resistant libpq
value quoting and verified configured TLS are tested without contacting host
dependencies. pgx5.11.0 and its transitive pins passed vulnerability/license checks.

Actual business storage/notification adapters, PostgreSQL admin-shutdown retries,
supervision and graceful drain remain unfinished; the consumer is deliberately
not started by Service.Run until complete handlers exist. Native libpq sslmode
default prefer can fall back to plaintext and does not authenticate a TLS server;
this inherited serious concern is preserved in options and needs operational
verify-full configuration. Configured verify-full is not bypassed. Native Rabbit
job publishing leaves delivery mode unset and has no publisher confirms; this
can lose accepted-looking child jobs on broker/channel failure. Member writes
precede notifications, so retries can see the member already added and skip a
notification that previously failed. No new outbox/durability contract is invented.
