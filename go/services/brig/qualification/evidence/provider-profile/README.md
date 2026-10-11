# Provider account/profile qualification

Observer source `936aef22374fb56e3cd111a96acc369e159d32c1`, production build source
`69e6c9381685b26d549585800683ee31e960d735`, SHA256
`61d082a916294925d04dd3cb8f1835c51381890c01fee52349b0942b71fbaf8b`. Full Go1.26.9 format/tidy/verify/vet/unit/race/build/
vulnerability/license checks and static units in the unchanged native runner
pass for source69. Subsequent observer/plan-only edits reuse these exact inputs;
production/tagged receipts preserve the actual compiler revision.

Implemented GET/HEAD /provider, GET/HEAD /providers/:id and PUT /provider using
real native-schema Cassandra rows, optional unlogged updates and global Galley
sndFactorPasswordChallenge guard. Provider account routes require native
Z-Type=provider plus Z-Provider; public profiles require Z-User. No password/hash
is selected or serialized. Native activation visibility and public email are
preserved; unrestricted description Text and gateway trust remain inherited
concerns rather than silent policy changes. New registration/activation/login/
password/reset/deletion/service/bot flows remain unfinished.

All14 actual native/Go status/media/JSON-or-body comparisons match: missing/wrong
provider token type, activated account/profile/HEAD, missing account/profile/
write, null/partial update, unactivated account/profile/write. Native HTTP creates
and activates the provider and creates a second unactivated row; Go writes name,
URL and description, and restarted native reads the exact Go-written account and
public profile. Unit tests cover feature denial/failure before storage, scoped
writes/header admission, native name/HTTPS/null/type rules and read/write error
media. Comparator status/media/JSON/body negatives passed locally.

All22 exact original MLS/connection executions pass with no failure/error/skip;
case names match previous qualification. Existing actual optimistic-Dynamo/STOMP
queue/service/bot/Galley controls and client/prekey/MLS/connection switchback pass
on the same executable. All25 managed exits0, runner0/cleanup0 and independent
owned resources empty. This is two-Go/Haskell remainder, not final535/eight-Go,
three-selector/all-entity/operations/image/selector or whole-Brig qualification.

Retain failures: initial candidate batch retry syntax and test UUID parser compile
errors; first native provider requests missing the required Z-Type (Go guard
corrected); observer comparison initialized too late; subsequent closure name
shadowing. Captured native responses also required Go read404 charset correction.
Failed backend process exits were all0 and owned cleanup succeeded. One failed
controller labeled reused production as built at observer961; its actual source
b26/SHAa525 is identified by retained-type-build-receipt.json, not its mislabeled
raw result. Failed runs remain failed. No Haskell or protected original assertions
were changed. Same-author review under user delegation verifies this finite slice
and receipts; it is not independent review or whole-service approval.
