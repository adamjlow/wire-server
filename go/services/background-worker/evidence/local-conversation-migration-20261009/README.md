Local conversation migration connected construction checkpoint

Pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass for exact source-files.json inputs. go-check.log retains successful
output and unchanged informational assembly/license-URL warnings. No new
Haskell, oracle, schema, dependency or selector changes; no full parity claim.

Both native conversation migration scans are connected under the original flag:
local conversations and remote membership. Local copy uses validated paged IDs,
bounded per-page execution, ten-second exclusive UUID lock and independent row
timeout. Typed reads preserve inactive/null membership distinction, service/provider
pairing, raw stored role text, child-before-parent deduplication, status/ref fields,
legacy/v2 access types, native access defaults and role-set ordering. Invalid
inactive/duplicate rows fail before filtering; fresh API role checks stay strict.
PostgreSQL stored role reads now accept the same raw UTF8 text as Cassandra.

Main MLS/mixed requires present group ID; Proteus drops unrelated raw MLS fields.
Active main MLS requires epoch/time/cipher together, including epoch0; native
Word16 cipher casts are canonicalized. Subconversations are sorted by source Map
order; epoch0 produces NULL cipher, epoch0 and Unix1970 timestamp after required
source decoding. Group-state NULL vs present empty bytes is preserved. Native
regular/history client lookup is required even without main group info, but main
client rows are copied only with that info. Sub clients always participate.

ReadCommitted transaction writes conversation/local members/remote members/subs/
MLS clients/pending ledger in native order and with native per-table conflict
rules. Empty native vectors stay nonnull empty arrays and nullable UUID/status
values stay nullable. Commit precedes Cassandra subgroup/main-client cleanup,
team index cleanup and existing source year3000 tombstone/member reverse-index
deletion. Ledger completion stays within local lock/row timeout; missing/incomplete
records still complete/count as native does. Failures stop subsequent effects.
Pending group-sync cleanup reuses the same full reader, including PG history,
and preserves pending ledger on failure. Focused tests cover these boundaries.

Actual original/DB migration execution is not part of this construction receipt;
next run existing original background-worker-mls with a clean identified static
binary. Prior users2 pre-enable SCIM500 remains unresolved/not waived. Scheduled
runtime, final original47/six-Go124, durable switchback/operations/image/selector
and delegated review remain required. Haskell default; goal/task active.
