Scheduled reaper construction and actual isolated PostgreSQL proof

Pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license checks
pass for exact source-files.json inputs. No new dependency, Haskell/reference/
assertion/schema change, selector or scanner suppression. Informational assembly
and local-license URL warnings remain retained. Test executable identity is in
build.json; it was built CGO0 with explicit schemacheck tag from declared inputs.

Native Arbiterb9c57eb1f8277d97616aa449bea471fe9ce14eda SQL17 templates are adapted
only for bound pgx parameters and fixed queue identifiers. Parent MIT notice and
individual source citations apply; original file SHA256s accompany templates.
Five native operations are implemented: group refresh, stale worker sweep,
exhausted tree-aware DLQ, cancelled lease-lapsed cleanup and expired archive purge.
No admission pruning/reconciliation is started for the registry's empty policies.

Schema-wide gate rows use native ensure/read/ReadCommitted transaction/SKIP LOCKED
claim/work/watermark commit behavior. Statement_timeout is transaction-local with
native ceiling-to-ms semantics, independently of whole-action context lifetime.
Only committed work advances the gate; failure rolls back the watermark. Queues
run in registry order. Native per-operation/per-job savepoint boundaries and caught
queue failures are preserved. Group refresh locks existing keys separately before
recomputing counts/leases and repairing missing group rows. Stale sweep includes
paused workers and uses each row's threshold. Exhausted/cancelled scans cap1000;
archive purge caps10000. Live leases and suspended/nonexhausted states are retained.

DLQ moves preserve all native carried columns and optimistic attempts. Rollup
moves first snapshot descendant rollups, cascade all descendants, then move the
root. Snapshot merge uses native Either JSON, error > result > prior snapshot
precedence, malformed-whole-snapshot discard and NULL error fallback. Parent
resume uses the same namespace/id advisory transaction lock as native sibling ACK,
with no remaining children before waking. Cancellation locks descending ids,
rechecks flags, deduplicates/sorts parents and shares native resume semantics.
Focused unit tests cover templates/queue injection/lease bounds and snapshot types.

Actual Docker-only TestScheduledReaperDockerStorage passes on isolated PostgreSQL:
all five operations, lease/suspension guards, paused stale sweep, corrupt group
count repair, cancelled-child parent wake, exhausted tree/descendant snapshot
preservation and expired-only archive cleanup. Immediate rerun is suppressed by
native shared interval; concurrent loser uses SKIP LOCKED and never runs work.
A real pg_sleep triggers per-statement timeout; rollback leaves gate eligible and
removes timeout configuration. Exact fixture images/static test binary and
cleanup receipts are retained. Nonroot capability-free readonly test process,
internal network/no host ports, synthetic inputs only. Containers/network/volume
are removed and owned labels empty; no live handles remain.

Limits: the reaper pass is constructed but not started until scheduled runtime
exists. This proves actual Go storage effects against source-derived requirements,
not fresh Haskell reaper execution or complete scheduled parity. No broad original
suite rerun for an unconnected component. Cron, adminless/meetings handlers,
scheduled runtime/startup/drain, final original47/six-Go124, full durable workload
switchback, operations/image/selector and delegated review remain required.
Users2 pre-enable SCIM500 remains unresolved/unwaived. Prior exact original MLS1
and gundeck40 passing proofs remain limited to their identified artifacts.
Whole-service goal/task active; Haskell default; no compatibility/availability.
