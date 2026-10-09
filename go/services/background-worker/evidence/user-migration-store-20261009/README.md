User-migration storage construction (runtime not connected)

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks passed for source-files.json inputs. Actual output is in go-check.log.
No new dependency. Actual PostgreSQL/Cassandra effects remain unexecuted.

Source Wire.UserStore.Migration and .Migration.Types plus UserStore.Postgres
asset statements define the record and branch effects. Serializable transaction
contains duplicate PG handle check, either deleted_user (deleted nonbot) or
wire_user ON CONFLICT DO NOTHING, optional asset replacement, live service bot
conversation insert and the pending-deletion ledger. Nullable fields/JSON/UUIDs
remain distinct; absent assets do not replace PG assets, present empty assets do.
Deleted bots retain a deleted wire_user without a bot_conv insertion. A duplicate
PG handle changes only the inserted tuple, while native cleanup still uses the
original validated Cassandra handle. User/password/handle fields are bound;
raw DB errors are replaced by generic diagnostics with SQLSTATE retained.

Copy/cleanup order is explicit: successful PG commit including pending ledger,
sequential Cassandra LocalQuorum handle/service-user/service-team/rich-info/user
deletes, then PG mark-complete. A failed copy never cleans Cassandra; failed
cleanup keeps the ledger; only complete successful effects count a copied user.
Tests cover native branch effects, nullable/empty asset distinctions, duplicate
handles, failure ordering, missing pending ledger and secret-safe diagnostics.
This is constructed behavior with fake storage boundaries, not actual transaction
or durable DB proof.

Hasql.Transaction.Sessions.transaction in source pinned hasql-transaction1.2.2
retries SQLSTATE40001/40P01 inside the same session. The candidate now retains
that behavior on the acquired PG connection; other PG usage failures propagate
to migration rescan and the existing admin-reset session retry policy is reused.
Context cancellation stops retry. Source package pin is nix/manual-overrides.nix;
primary source hasql-transaction-1.2.2/src/Hasql/Transaction/Private/Sessions.hs
is in the versioned package tarball:
https://hackage-content.haskell.org/package/hasql-transaction-1.2.2/hasql-transaction-1.2.2.tar.gz

User row decoding, user/service/claim/rich-info reads, source invalid-row/asset
handling, locked bounded parallel scan and runtime startup remain unfinished.
MigrateUsers still explicitly rejects startup. Five other families remain
connected, conversations also unfinished. Scheduled execution, final original124,
real DB migration/switchback, bounded operations/image/selector/delegated review
remain required. Earlier original40 proof applies only to its exact earlier
source00557161c5218fba7ce290ba57536561298a35e3. No current compatibility or availability
claim; native local Haskell build checks remain unavailable/unrun.
