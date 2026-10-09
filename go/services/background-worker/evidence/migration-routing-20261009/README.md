Migration routing and real background-job consumer construction

Full Go1.26.9 checks passed. A subsequent bot-lifetime adjustment preserves
callbacks through Rabbit reconnects; final vet/race tests pass on those source
inputs, identified in source-files.json. Unchanged dependency security/license
results reused. No final clean binary, original E2E or full qualification claim.

All seven required storage-location values are now typed/validated using native
cassandra/postgresql/migration-to-postgresql values. Missing/invalid fields reject
configuration. Conversation reads in migration mode take the native UUID-mixed
shared advisory lock with500ms acquisition budget (including pool wait), complete
pending Cassandra deletes, then read PG with fallback to Cassandra. Writes use
PG EXISTS, retaining the source distinction for unreadable/incomplete MLS rows.
One lock session stays held while normal pool sessions perform cleanup/read/write.
Unlock has a separate1s context; errors close the session before releasing it to
the pool. pgconn.Close always closes the underlying network connection, even
with an expired context, preventing leaked advisory locks in pooled sessions.

Pending ledger completion occurs only after successful Cassandra cleanup;
failed cleanup/partial effects retain the ledger for idempotent retry. Cleanup
loads/validates subconversations, group state and MLS clients before deleting
subgroup/parent MLS rows, team index and conversation/membership reverse indexes.
Native future tombstones and source retries remain. Tests verify lock-key vectors,
source routing/EXISTS distinction, cleanup ordering and failed-ledger completion.
Actual PG lock contention and durable effects still require owned Docker proof.

The actual Rabbit job consumer now starts with real PG/group/Cassandra stores,
Brig bot lookup, local Gundeck and remote Rabbit notifications. Publisher channel
is separate from the consumer. Bot callbacks belong to process lifetime and drain
before Brig closes, rather than blocking Rabbit reconnect. Construction still
needs consolidated pre-health startup, logs, graceful acquisition-stop/drain,
Arbiter scheduled jobs and all seven migration loops. Enabled migration flags
continue to fail startup explicitly. No supported final feature is silently
claimed complete. No changes to protected original assertions or schemas.

Next rerun original unchanged gundeck40 with only Go worker and Haskell peers
using a fresh clean static binary receipt, then complete remaining whole worker.
Final six-Go original124/ops/durable switchback/image/selector remain mandatory.
