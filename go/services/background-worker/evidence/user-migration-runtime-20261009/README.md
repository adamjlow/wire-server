User migration connected construction checkpoint

Full pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks pass for exact inputs in source-files.json; go-check.log retains output
and unchanged informational assembly/license-URL warnings. No new dependency,
Haskell/oracle/schema change, scanner suppression or full parity claim.

The user migration now reads bound LocalQuorum Cassandra rows without extra
retry policy, preserving NULL asset UDT fields through the existing custom
collector. Native SSO JSON text decodes to either SAML or SCIM, drops unknown
JSON properties, handles nullable optional keys, and serializes canonical XML
for PG. Three unchanged original wire-api JSON goldens are read directly by the
unit tests: exact Issuer/NameID XML strings, unusual Unicode/control SCIM and
empty SCIM. Legacy issuer URL/subject email and plaintext, XML formats/qualifiers,
source persistent/entity-only length limits, native qualifier-discard quirk,
CDATA/text/escaping and arbitrary schemes/lax query storage are constructed.
Go XML performs no external entity/file/network resolution. Short malformed XML
can still use the native plain-Name fallback, escaped as content. Native library
execution is unavailable; golden/source checks are not the full Haskell suite.

Related service conversation/team, handle claim and rich-info reads precede the
missing-required-user skip. Only a handle actually claimed by this user survives
into PG and source cleanup; another user's claim is not deleted. Present empty
assets remain distinct from absence. Focused tests prove parameter binding,
related-read order, malformed typed related/SSO failures before eligibility skip,
exclusive-lock placement, row timeout after lock, copy commit before CQL cleanup
and pending-ledger completion, and no effects for disappeared users. Previous
Serializable transaction/retry/cleanup and nullable driver tests remain green.

MigrateUsers is now connected to Brig Cassandra and PostgreSQL with a validated
paged ID scan, bounded parallel execution, native ten-second exclusive UUID lock
and independent row timeout. All copy/cleanup/complete actions stay locked.
Six migration families are connected; conversation migration still rejects
startup. Actual user DB effects and original migration assertions remain unrun
at this checkpoint; execute the existing original background-worker-users
profile with an identified clean static executable next. Scheduled runtime and
final six-Go124, durable switchback, operations/image/selector/review remain
unfinished. No availability/compatibility claim; Haskell remains default.
