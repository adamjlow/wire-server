Partial migration and scheduled acknowledgement construction

Pinned Go1.26.9 full format/tidy/verify/vet/unit/race/build/vulnerability/license
checks passed for source-files.json inputs; go-check.log retains output. No new
dependency. This is construction evidence, not actual PostgreSQL/Cassandra or
whole-service parity evidence.

Block-list and password-reset migrations follow Wire.BlockListStore.Migration
and Wire.PasswordResetCodeStore.Migration and their source Postgres/Cassandra
interpreters: entire typed pages validated before copies, LocalQuorum/x5 paging,
positive remaining TTL and native reset alphabet, expired/legacy reset skips,
bound upserts retaining Cassandra, PG usage-error full rescans, timeout/fatal
stop until restart, source counters and outcome durations. Configured loops
are connected; the remaining five migration flags still explicitly fail startup.
Tests cover full rescan counting, fatal timeout, malformed page atomic decoding,
secret-safe bound values and native nullable/TTL handling. Actual DB loops have
not been exercised. Startup resource sharing/supervision/drain remain unfinished.

Scheduled ACK adapts pinned Arbiter b9c57eb1f8277d97616aa449bea471fe9ce14eda
Arbiter.Core.Sql.Ack: optimistic attempts, parent/sibling advisory serialization,
parent suspension/wakeup and optional archive snapshot/retention. MIT notice is
retained in arbiter_sql. It is not connected or exercised against PostgreSQL;
schema bootstrap, reaper, cron and domain handlers remain unfinished.

Original40 passed on the distinct earlier source00557161c5218fba7ce290ba57536561298a35e3
in ../jobs-original-20261009. That executable contains none of this checkpoint.
No final qualification, compatibility or availability claim is made.

Correction at the later code-migration checkpoint: native Cql AsciiText bypasses
API alphabet validation (Data.Text.Ascii.fromCql uses unsafeFromText). The earlier
alphabet claim above was incorrect. Later implementation/tests correct this;
see ../code-migration-20261009. Retained source hashes/checks refer to the older
implementation and do not establish current parity.
