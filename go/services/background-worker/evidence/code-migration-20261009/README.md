Conversation-code migration construction

Pinned Go1.26.9 full format/tidy/verify/vet/unit/race/build/vulnerability/license
checks passed for source-files.json inputs. go-check.log retains output. No new
dependency; no actual database migration or final executable qualification here.

Source Wire.CodeStore.Migration, .Cassandra.Queries and .Postgres are reused as
the contract: LocalQuorum/x5 explicit paging, typed entire-page decoding before
writes, key length20/value length6..20, required UUID and Int32 remaining TTL,
expired-row skip after decoding, nullable password, bound conversation-target
upsert refreshing expiry from PG now()+remaining CQL TTL, retained Cassandra.
Source full rescan on PG usage errors and fatal timeout use the existing engine.
The configured conversation-code loop now uses Galley Cassandra alongside the
Brig block-list/password-reset loops. Four remaining migration flags reject
startup explicitly until their implementations exist.

Wire.API.Password.{Argon2id,Scrypt} parse and canonicalize legacy hashes before
writing PG. The Go codec retains optional Argon2 version16 default, supported
variants/versions, duplicate option last-value behavior, native Word32 numeric
conversion, strict Base64, fixed parameter ordering and canonical padding. It
only decodes/re-encodes hashes: no password/KDF work, no secret logging. Tests
use synthetic salts/hashes and check malformed stored data, nullable password,
TTL/target/bound values, native length ranges and canonical legacy encodings.
Underlying types/strict Base64 were checked against these primary sources:
https://raw.githubusercontent.com/kazu-yamamoto/crypton/crypton-v1.1.2/Crypto/KDF/Argon2.hs
https://hackage-content.haskell.org/package/base64-bytestring-1.2.1.0/src/Data/ByteString/Base64/Internal.hs

Correction from the prior identity construction checkpoint: native Cql
AsciiText.fromCql uses unsafeFromText, bypassing the API Base64 alphabet check.
Password-reset stored key/code decoding now accepts the native CQL text, and
conversation-code decoding adds only the native Range length requirements.
Focused tests preserve nonalphabet text. The preceding checkpoint's alphabet
claim was incorrect; its source/log hashes remain historical evidence, not a
claim of parity for that older implementation.

Actual PG/Cassandra behavior, scheduled runtime, final original124, operations
and durable Haskell-Go-Haskell switchback remain outstanding. Original40 proof
in ../jobs-original-20261009 applies only to its earlier exact source00557161c5218fba7ce290ba57536561298a35e3.
