User-migration value decoding construction (runtime not connected)

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks passed for the Go inputs and original fixture cases in source-files.json;
go-check.log retains output. Fixture descriptor field was renamed from
unimported_nonliteral_cases to unimported_cases after checks; its tested case
values/assertions are unchanged. No new Go dependency and no actual DB execution.

Wire.UserStore.Migration.Types, User.Profile, Locale, User.RichInfo and Asset
source contracts drive decoding. Scalar fields retain nullable false/zero, native
accent0/userType regular defaults, UUID absence, account/managed enum validation,
protocol-mask unknown-bit filtering, password canonicalization, nullable picture
and exact activated microsecond write-time. Typed errors are not hidden by the
missing name/activated/writetime eligibility skip. Stored Name/Handle/TextStatus
are native Text newtypes, not stricter API schema inputs.

Custom gocql UDT decoding preserves nullable asset typ/key/size fields. Actual
CQL collection bytes marshaled by the independent ordinary driver path are read
through the custom decoder: NULL type stays absent (invalid asset skipped), NULL
size stays absent (not invented preview0), positive valid sizes remain. Absent
vs empty asset lists remain distinct for PG replacement. Asset version/retention
Word decimal and UUID text canonicalization match the source. Invalid assets
are omitted while preserving the user; warning contains no raw asset/user data.

Rich-info decoding checks source version0/case-insensitive outer keys, required
field types, case-folded duplicate types before empty-value removal, then native
normalized JSON. Picture lists decode blobs as JSON Objects, preserve absence
and empty lists, and do not introduce API list limits. Country/language enum data
is pinned to source iso3166-country-codes0.20140203.8 (249) and iso6390.1.0.3 (184),
not the newest OS locale tables. Native parseOnly accepts a valid two-character
prefix with a suffix; case and enum membership are preserved. Package source
hashes/URLs and version evidence are retained; these are factual ISO enum data.

Email grammar/canonicalization adapts native email-validate2.3.2.21: comments/
folded whitespace stripped, quoted local parts/escapes preserved as source,
terminal domain dot discarded, canonical byte-length limits and native domain
literal acceptance. testdata/native-email-examples.json imports203 original
literal valid/invalid expectations unchanged; every imported case plus native
idempotence expectation passed. Seven source cases were not imported (nonliteral
expressions/Haskell-specific escaping), explicitly counted; this is not a claim
that the entire native package test suite was executed. Additional concrete
canonicalization/length/malformed cases passed. Original BSD3 notice/copyright is
retained in third_party/email-validate and must accompany the eventual image
alongside URI BSD3 and Arbiter MIT notices.

Primary package sources:
https://hackage-content.haskell.org/package/email-validate-2.3.2.21/src/src/Text/Email/Parser.hs
https://hackage-content.haskell.org/package/email-validate-2.3.2.21/src/tests/Main.hs
https://hackage-content.haskell.org/package/iso3166-country-codes-0.20140203.8/src/Data/ISO3166_CountryCodes.hs
https://hackage-content.haskell.org/package/iso639-0.1.0.3/src/Data/LanguageCodes.hs
Reference-build retained Nix dry-run names these exact package versions.

Still required: SSO decoding, complete typed user construction, user/service/
handle-claim/rich-info Cassandra reads, claim-based handle retention, bounded
parallel exclusive-locked scan/runtime wiring. MigrateUsers and conversations
remain explicitly rejected; five other migration families are connected.
The new value helpers are not connected or exercised against actual DB storage.
Scheduled execution, final original124, real durable switchback, bounded operations,
image/selector/delegated review remain required. Original40 proof applies only
to earlier exact source00557161c5218fba7ce290ba57536561298a35e3. No final qualification
or compatibility/availability claim; native Haskell builds remain unavailable.
