Conversation storage and notification construction

Go1.26.9 full module checks passed. A subsequent response-read error guard was
then checked with go vet and final race tests; dependency/security/license inputs
were unchanged and their passing checks are reused. source-files.json identifies
final source inputs; no clean final artifact or actual job E2E claim is made.

Implemented source PostgreSQL read-committed/read-only conversation reads with
all18 metadata columns, parent/local/remote membership and child-first dedup.
Local member inserts/upserts use source read-committed write transactions and
bound UUID/role arguments. Administrative shutdown SQLSTATE57P01-04 permits
five pool reset/retries then a sixth attempt, matching Wire.Postgres. Other errors
are not retried here; raw server errors are replaced by generic messages.

Join notification construction uses original event fields and SCIM/user actor,
millisecond local time, modern FromV1 and legacy UntilV1 remote forms, persistent
single-active quorum notification queues, source four enqueue attempts/one-second
delay, remote buckets/concurrency8 and origin-domain suppression. HTTP tests
validate source chunked Gundeck push/no replay, default fanout2000/chunks128,
including empty-recipient Cells delivery. Source Domain permits localhost and
normalizes case; notification origin/target and loaded domain now match that.

Still required: actual bot delivery with source RSA public-key pins and certificate
validation, all Cassandra/migration storage modes, connecting/supervising the job
consumer, startup consolidation and graceful drain. PostgreSQL/database effects
are implemented but have not yet been exercised by original group E2E cases.
Native TLS bot verification permits valid self-signed pinned keys and lacks
hostname verification; preserve the authenticated pin/date/chain contract,
document the hostname limitation, and never substitute an unchecked TLS client.
The protected original tests and reference schemas are unchanged.
