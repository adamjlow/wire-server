# Shared-schema connection reads and native opaque cursors

Qualified source `2979e82ec3eeb89cbd37c36d83033c67bcd77dcf`, Brig SHA256
`ca96ee7c367d21a3895f0811795713db350d0a31bab7cc8b8025a50de5c3f52b`.
Pinned Go1.26.9 format/dependency/vet/unit/race/security/license/build and
static normal unit tests pass. Shared schema local/remote reads use native
LocalQuorum/retry1 and exact Cassandra statements/cursors. Historical relations
and nullable local conversation serialization follow native source. Mutation
store primitives retain native LocalQuorum/retry5 but are not HTTP-bound yet.

Native APIs create three activated local targets and one activated remote target,
then create actual connections and Galley conversations. Eleven response controls
compare qualified local/remote and legacy individual reads, absent404, legacy
list/default/size1/size2/start and modern default list. All status/media/JSON
match. Five size1 pages traverse every local row then the remote row and terminal
empty page; every Go-issued table/Cassandra cursor byte matches native exactly.
Native restarts on unchanged stores and explicitly uses Go-issued cursors for
five matching pages, retaining IDs, relation, conversation and timestamp fields.
No direct connection-row injection, offsets, schema changes or normalized tokens.

All14 unchanged original compiled MLS tests rerun against this exact executable;
strict expected-name XML14 has no failures/errors/skips, SHA256
`5d568e63db73634a8d162709f6d542ba3e463adb7b37d9940385bbb11d67895b`.
Existing paired client/prekey/MLS controls and actual signed/raw state switchbacks
also pass. All25 managed process exits0; runner0/cleanup0 and independent owned
project container/network/volume audit empty. No forced stop.

Retained first attempt at source1aad93a08 failed only absent404 media: native
text/plain;charset=utf-8 versus omitted Go header. Ten other controls and all
five cursor pages matched; originals were not reached. All24 exits0/runner1/
cleanup0/independent empty. Candidate header corrected with focused regression;
no native source, original assertions or protected fixture was altered.

Same-author scoped review under the user's explicit review delegation accepts
this read/storage increment and evidence; it is not whole connection or Brig
approval. Create/update state machines and activation/team/legalhold/limit/app
admission, Galley membership and notification effects, incoming/outgoing remote
connection actions and remaining parser/version/error checks are incomplete.
Whole535/eight-Go/all-entity three-mode/operations/image/selector/legal/security
acceptance remains open. Read proof is configured native user mode only. Haskell
remains supported/default; this continuation is committed locally, not pushed.
