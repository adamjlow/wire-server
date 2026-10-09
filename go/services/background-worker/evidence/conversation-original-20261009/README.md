Original Haskell MLS conversation migration: pass

Exact source309733bb48febdb8708e6b1c5aadb94cb03fe8ab / clean static Go1.26.9
CGO0 binary9e21f14e4f11d203faa98e71bea7490c5c2d172fa879e67da73bf47275035f8f.
Unchanged original Migration.Conversation.testMigrationToPostgresMLS passed1/1,
no skips/errors/failures, with only Go background-worker and Haskell peers.
The original test traverses five storage phases, enables local/remote migration
in phase3, waits for both native completion counters, and validates shared MLS
history in the final PostgreSQL-only phase. This is actual conversation storage
and interoperability evidence, including enabled copying/deletion and PG reads;
it is not a construction-only test or invented replacement assertion.

The previous run's native counter-text mismatch is retained separately in
../conversation-original-initial-20261009; candidate decimal formatting corrected
without changing the Haskell test or harness. Corrected full Go checks are in
../migration-metric-format-20261009. Build/environment/suite/XML receipts here
identify the actual artifact, dependencies and runner. Controller0/runner0/
cleanup0; post-cleanup owned container/network/volume labels empty.

Limited coverage: this does not qualify other migration families, full worker47,
final exact six-Go124, scheduled jobs, bounded operations or actual complete
Haskell-Go-Haskell workload switchback. Users2 pre-enable SCIM500 remains unresolved
and unwaived. Haskell stays default; task and full-service goal remain active.
