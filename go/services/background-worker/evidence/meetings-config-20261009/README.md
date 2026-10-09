Meetings required configuration and retention cutoff construction

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass for declared inputs. No dependency, upstream/oracle/schema/selector
change or scanner suppression. Informational assembly/local-license URL warnings
remain retained. No Docker/process was started in this checkpoint; earlier exact
meetings storage inputs are unchanged and their limited PostgreSQL proof is reused.
Native full Haskell checks remain unrun; no clean final service qualification.

LoadConfig now decodes required meetingsCleanup values, validates nonnegative
finite retention, positive integral batch and source-valid cron, rejects absent/
NULL or wrongly typed values and numeric/string coercions. Unknown fields retain
native allowed behavior. Native parse+serialize CronSchedule normalizes code
expressions (macros, names and leading zeros), keeping range/list/star-step
structure rather than rebuilding from expanded match sets.

Configured cleanup computes one cutoff before the batch loop. Native Double->
Pico floors before multiplying3600; arbitrary integers preserve this order and
avoid Go Duration's ~292-year limit. PostgreSQL Binary's floor-to-microseconds and
Int64 conversion are preserved, then represented as a Go UTC value for pgx.
Tests cover zero/fractional retention,1000-year retention, extreme finite wrap,
required types/canonical cron and configured cutoff/cancellation. Independent
Python Fraction-derived wire vectors document arithmetic, not native execution.
Initial focused test failed because its1000-year expected date was incorrectly
calculated; independently checked date is1026-10-02. Only candidate test expectation
was corrected; no protected oracle/candidate behavior was weakened. Full checks
then pass. Initial failure is retained.

Inherited concern: an enormous finite retention can wrap PostgreSQL Binary's
Int64 timestamp back into a recent cutoff (1e300 hours wraps back to the current
microsecond timestamp in this model), risking unintended expiry. Source configuration
has no finite upper bound. Candidate mirrors reviewed source arithmetic; do not
silently impose a new policy. Record and review a separate bound/encoding fix if
requested. Primary source snapshots/hashes are retained; current master/time and
postgresql-binary0.15.0.1 are source comparisons, not freshly identified native
GHC package versions. Final reference qualification remains necessary.

Concrete unchecked conversation/proposal/MLS deletion, adminless handlers and
whole scheduled runtime/NOTIFY/metrics/startup/drain remain unfinished. Original47/
six-Go124, durable switchback, bounded operations, optional image/selector and final
delegated review remain mandatory. Users2 SCIM500 remains unresolved/unwaived;
prior exact gundeck40/MLS1 proofs stay limited. Goal/task active; Haskell default;
no compatibility/availability; no live handles/resources.
