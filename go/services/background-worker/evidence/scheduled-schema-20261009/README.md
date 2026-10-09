Scheduled schema construction and actual isolated PostgreSQL proof

Pinned Go1.26.9 full format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass for declared source-files.json inputs. No new dependency, Haskell,
oracle, schema contract or selector changes. Informational scanner/license URL
warnings remain retained. Actual test executable identity is in build.json.

Arbiter revisionb9c57eb1f8277d97616aa449bea471fe9ce14eda supplies44 byte-identical
native migration SQL bodies and names; source SHA256s are in arbiter_sql/migrations/
manifest.json and existing MIT notice applies. Native registry order produces76
scripts:12 schema,32 meetings,32 conversations. Native default notify triggers
are included; default-disabled event streaming and undeclared admission triggers
are excluded. No policy/override rows are deleted during default reconciliation.
Bodies render only fixed native queue identifiers, and checksums use the existing
Base64(MD5(body)) representation, never hexadecimal hashes. This is a compatibility
ledger checksum, not authentication. Dedicated session-scoped native Text advisory
lock covers setup, with60s wait/1s polling. Native ReadCommitted migration transaction
commits preceding successful scripts on checksum mismatch (returned error), while
SQL exceptions roll back. Existing schema privilege fallback, default unlogged
rate-limit durability and native adminless team partial index are preserved.
The bootstrap function is not started until scheduled runtime is connected.

Actual Docker-only tagged qualifier uses fixed synthetic PostgreSQL/fixture target,
internal owned network/no host ports, readonly nonroot capability-free test process
and readonly original native column capture. All227 captured native column records
match exactly, including defaults/order/types/nullability; no normalization or
oracle edits. All76 ledger entries are written. A grouped meetings job exercises
native triggers and survives bootstrap rerun. Injected checksum mismatch stops
setup and preserves the native preceding-success commit. Test exit0 and owned
containers/networks/volume empty after cleanup. docker.json pins images, binary
and original capture SHA. Compile with CGO_ENABLED=0 go test -c -tags schemacheck;
run only TestScheduledSchemaDockerStorage in the described owned fixture.

Initial isolated fixture checked only its temporary initdb unix socket and raced
TCP startup, so the qualifier failed opening its lock connection; retained initial
log/receipt show cleanup success. TCP127.0.0.1 readiness corrected the fixture;
no candidate code/test expectation was changed or failure waived. Current check
is terminal with all owned resources removed. No expensive original suite rerun
because the scheduled bootstrap is not connected to the service yet.

Limits: captured-schema construction/actual Go DB proof is not fresh Haskell
migration execution, complete scheduled execution or Haskell-Go-Haskell switchback.
Same-ledger full coexistence, scheduled claims/cron/reaper/domain handlers and
runtime/startup/drain, final worker47/six-Go124, operations/image/selector/delegated
review remain required. Original MLS1 and gundeck40 receipts remain limited exact
artifact evidence. Users2 pre-enable SCIM500 unresolved/unwaived. Goal/task active;
Haskell default; no whole-service parity or availability claim.

Checksum/transaction source consulted:
https://github.com/andrevdm/postgresql-migration/blob/master/src/Database/PostgreSQL/Simple/Migration.hs
Native SQL and registry declarations are pinned by existing Arbiter source identities.
