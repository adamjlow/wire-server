Domain-registration and team-feature migration construction

Pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license checks
passed for source-files.json inputs; go-check.log retains actual output. This is
construction evidence. Neither actual migration DB execution nor final service
qualification is established here. Existing original40 proof applies to its
separate earlier source00557161c5218fba7ce290ba57536561298a35e3 only.

Domain source: Wire.DomainRegistrationStore.Migration, .Cassandra, .Postgres,
Wire.DomainVerificationChallengeStore.Postgres, Wire.API.EnterpriseLogin and
Wire.Migration.withExclusiveMigrationLockAndTimeout. Challenges run first with
positive remaining CQL TTL, typed Domain/DNS tokens and bound PG upsert; there is
no native challenge row timeout/histogram. Registrations use case-folded DomainKey
text, whole-page typed decoding, ten-second exclusive lock acquisition, then
configured row timeout; check PG existence before a current Cassandra x1 read
under the lock, upsert that current row only when still present/unmigrated,
preserve nullable fields and Cassandra. Both phases rescan together after caught
PG/lock errors; finished increments once for an entirely successful pass.
Timeout/fatal errors stop until restart. Source duration labels include lock_timeout,
timeout/error/success and successful skipped existing/deleted registrations.

Team-feature source: Wire.TeamFeatureStore.Migration and Cassandra/Postgres.Queries.
Typed nullable status/lock enums0/1 and arbitrary JSON DbConfig are preserved;
SQL NULL and JSON null remain distinct with the actual PG wire codec. Per-feature
exclusive lock combines source UUID words with rotated native Text hash. Check
PG existence under the lock, then bound COALESCE patch only when absent. Feature
names are stored text (including unknown/empty), not an invented registry filter.
Leave Cassandra rows for rollback; rescan on caught errors, fatal row timeout.

Native hashable-1.5.1.0 Text.hash uses XXH3_64 UTF8 seed0. Existing retained
reference-build Nix dry-run identifies that package version. Primary Class.hs
was inspected; independent-lock-vectors.json contains actual independent C
libxxhash outputs and the source hash. Candidate pins zeebo/xxh3 v1.1.0 (BSD2),
cpuid/v2 v2.2.10 (MIT) plus existing x/text case folding. Vulnerability check
reports no vulnerabilities. License tooling reports assembly inspection warnings;
manual source review found only Go textflag.h intrinsics and CPU operations,
not additional native library dependencies. Module/license checks passed.
https://github.com/zeebo/xxh3/tree/v1.1.0
https://hackage-content.haskell.org/package/hashable-1.5.1.0/src/src/Data/Hashable/Class.hs

Stored HTTPS URL blobs decode/re-encode using source uri-bytestring-0.3.3.1
semantics: path/query escaping, query-pair ordering/empty keys, fragment spelling,
userinfo/port serialization and IPv6 bracket removal. This is source-traced
construction; direct execution of that Haskell library is unavailable: a bounded
read-only/network-none runner probe found no ghc/compiler or URI/hashable packages.
Go tests cover the concrete serialization cases and malformed typed fields.
URI BSD3 notice is retained in third_party/uri-bytestring; eventual binary/image
must retain this and the Arbiter MIT notice. Native IPv6 serialization can produce
ambiguous/invalid unbracketed URL bytes; record it as an inherited data-format
concern rather than silently changing migration behavior.
https://hackage-content.haskell.org/package/uri-bytestring-0.3.3.1/src/src/URI/ByteString/Internal.hs

Five configured migration families are now connected; conversation and user
migration flags still explicitly reject startup. Scheduled runtime remains
unfinished. Actual PG/Cassandra effects/locks, scheduled execution, final original
six-Go124 all-pass, bounded operations and durable Haskell-Go-Haskell switchback,
image/selector and delegated review remain required. No compatibility/availability
claim or production switch. No retained Docker process remains from the probe.
