Conversation remote-membership and MLS construction checkpoint

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass for exact source-files.json inputs. go-check.log retains successful
output and unchanged informational assembly/license-URL warnings. Initial local
compilation found an unused import; corrected before passing final checks.
No dependency, Haskell/reference assertion, schema or selector change.

Native ConversationStore.Migration has independent local-conversation and remote
membership scans. Remote construction now has validated distinct-user pagination,
configured bounded per-page execution, ten-second exclusive UUID lock and separate
row timeout. ID enumeration uses native1001 lookahead/1000 retained items and last
retained folded domain/UUID cursor, then current status queries grouped by domain
with pool8. All lookahead IDs are typed before accepting a page. CQL uses bound
parameters, LocalQuorum and native no-extra-retry reads; partition deletion usesx5.
Nullable mute/reference fields remain absent vs zero/empty, archived/hidden NULLs
become false, and Domain/ClientId canonicalization follows native storage types.
Status rows are ordered by UUID then domain, matching Qualified's derived Ord.

ReadCommitted PG transaction copies statuses via nullable UNNEST columns then
writes the conversation_migration_pending_deletes user ledger. Native membership
INSERT intentionally has no ON CONFLICT; errors are not silently suppressed.
Serializable/deadlock transaction retries use the same pinned Hasql session
semantics as user copy, plus existing finite administrative-shutdown resets.
Commit precedes CQL deletion. The source releases lock/row timeout before ledger
completion; the new path preserves that distinction, including failure counting.
Tests cover cursor/lookahead, rereads/disappeared IDs, malformed lookahead, nullable
SQL bindings/conflict behavior, all step failures/ordering and eight-worker bound.

MLS construction reads typed Cassandra regular clients and existing PG history
clients. Native ClientMap contains only nonpending regular identities; IndexMap
uses earliest regular row per leaf then history precedence at colliding indices.
Copy excludes history entries, sorts signed indices and recomputes removal_pending
from active identity membership (including identities at shadowed indices).
Focused tests cover strict required values, canonical domain/hex client text,
signed Int32 leaf indices, history precedence/duplicate index resolution and
active-identity removal semantics. PG history rows use native bound lookup/reset
policy and sanitized errors. No fabricated Haskell/native package execution.

These are construction components: no actual DB/original migration execution in
this checkpoint, and neither remote scan nor MLS loader is started yet. Complete
local conversation/member/subconversation/group-state transactional copy and
cleanup before enabling MigrateConversations and starting both native loops.
Six prior migration families remain connected; scheduled runtime remains
unfinished. Prior original users2 failure at phase2 before migration activation
remains unresolved and unchanged, not waived. No expensive unchanged test rerun.
Final original47/six-Go124, actual DB/switchback/operations/image/selector/review
remain required. Haskell default, goal/task active, no compatibility/availability.
