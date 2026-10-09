Meetings storage and cleanup orchestration construction

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass. Tagged schemacheck fixture is excluded from ordinary checks and was
separately compiled CGO0/executed inside owned Docker. Declared inputs, binary,
compiler, images and terminal cleanup are retained. Informational assembly/local
license URL warnings remain visible. No new dependency, Haskell/schema/oracle
change, selector or suppressed check. Native Haskell compile/checks unrun.

Read full native meeting columns, validate stored UUID/title/timezone/type and
recurrence before effects. Preserve signed recurrence intervals, native handling
of NULL frequency, and absence of an effective end for open recurrence. Query
nonrecurring and bounded recurring branches separately using native strict cutoff,
partial-index predicates/order/limits. Stable effective-end merge retains branch
ties and global batch cap, after decoding both full branch vectors. Deletes use
bound canonical UUID/code keys, textual meeting: SHA256 first15 bytes/base64url.
Meetings always live in PostgreSQL. Code mode Cassandra explicitly ignores meeting
code deletion; migrating/PostgreSQL removes the native PostgreSQL key.

Cleanup performs conversation lookup, unchecked deletion only for matching id
and MeetingConversation, then code and meeting row deletion. No notification is
added: native unchecked action only performs storage/proposal/code removal. Full
batches repeat with one cutoff; partial batches stop. Subsystem failures stop the
run but still permit native runs-counter increment; partial effects in the failed
batch do not contribute to totalDeleted. Cancellation/invalid batch remains an
error, not a completed run. Concrete conversation/proposal deletion adapter,
retention config/cutoff calculation and runtime/metrics wiring remain unfinished.

Focused unit cases check ordering, matching/missing/nonmeeting guards, each failure
boundary, unchanged cutoff/full-vs-partial batches, cancellation, stored decoding,
stable merge and independently calculated code-key vector. No copied protected
oracle expectations changed. PostgreSQL qualifier mounts9 original migration SQL
files readonly, executes them unchanged and proves actual two-query merge/cap,
malformed branch row rejection even outside the global cap, strict boundary/open
recurrence exclusion, five actual SQL deletions across batches, code isolation,
idempotent rerun and explicit Cassandra-only code no-op with meeting deletion.
Conversation effects in this fixture are an explicit missing-conversation stub;
this is not execution of real conversation/proposal/MLS deletion.

Task-owned internal Docker network/no host ports, synthetic fixture only, nonroot
readonly capability-free static qualifier. All4 cleanup commands exit0 and owned
container/network/volume labels are empty. No dependency-bearing host process or
live handle/resource remains. Exact source hashes pin reviewed primary inputs.
Timezone decoding uses existing Go IANA resolver; no fresh native tzdata/version
comparison or Haskell cleanup execution is claimed in this checkpoint.

Whole scheduled runtime/adminless/meetings deletion integration, original47 and
exact six-Go124, durable switchback, bounded operations, optional image/selector
and delegated final review remain required. Users2 pre-enable SCIM500 is unresolved
and unwaived; prior MLS1/gundeck40 proofs stay limited to their exact artifacts.
Goal/task active, Haskell supported/default, no compatibility/availability claim.
