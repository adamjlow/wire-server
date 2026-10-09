Bounded migration page execution (construction checkpoint)

Pinned Go1.26.9 complete format/tidy/verify/vet/unit/race/build/vulnerability/
license checks pass for the exact inputs in source-files.json. go-check.log
retains the successful checker output, including unchanged informational assembly
inspection/local-license-URL warnings. No dependency change, scanner suppression,
new external interface or database execution.

Native UserStore.Migration.migrateAllUsers and ConversationStore.Migration's
conversation/remote-user scans use configured pooled concurrency per page.
Wire.Sem.Concurrency.IO delegates that pool to UnliftIO. The Go migration engine
now supports this explicitly per phase, leaving the five connected families
sequential as before. Worker count is bounded by configured parallelism and
current page length. Every page joins before fetching the next. Fatal row errors
cancel and join siblings; caught PostgreSQL/lock failures finish the page and
trigger the original full-phase rescan. Atomic error accounting and existing
mutex-protected metrics avoid races; per-row configured timeout behavior stays
unchanged. Existing typed-page validation still runs before any copy.

Focused channel-coordinated tests prove two simultaneous bounded workers, page
completion before the next fetch, fatal cancellation with joined cleanup, no
remaining-row starts after fatal cancellation, and PG/lock-error full rescans
with correct successful/finished/failed counts. Existing sequential migration,
timeout and malformed-page tests also pass in the full race suite.

This checkpoint does not enable user or conversation migrations: their complete
Cassandra decoding/storage/cleanup scans must invoke the new parallel phase once
finished. User SSO/related reads and the conversation migration remain unfinished;
scheduled jobs remain disconnected. No real DB, original worker47, final six-Go124,
loaded operations, durable switchback or image/selector qualification is claimed.
Original40 evidence still applies only to its earlier identified artifact.
Whole-service goal and implementation task remain active; Haskell stays default.
