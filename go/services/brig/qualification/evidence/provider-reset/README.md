# Provider password-reset qualification

Production `3c9f3deae0314c9626345243c195f56fc8559d28`, observer
`2d3cea1cbd69ddf4e311c6ac0992221f8169ef47`, executable SHA256
`c40e6d486e843e379356108e5b9a8cc64e200c42c40200e1a89256bf7c8fdf70`.
Pinned Go1.26.9 SDK/native runner/archive identities and compiler source are in
receipts. Full format/tidy/verify/vet/unit/race/build/vulnerability/license checks
and static original-runner normal units pass. Observer/document-only fix reuses
exact unchanged production/test/dependency/compiler inputs; actual build source
is preserved rather than relabelled as the observer revision.

Implements POST /provider/password-reset, POST /provider/password-reset/complete
and GET/HEAD /i/provider/password-reset-code?email. Native normalized email key,
vcodes scope3, three attempts, one-hour storage TTL, configured generation
throttle, parameterized LocalQuorum reads retry1/writes retry5. Pending exhausted
codes take precedence over throttle. Generation is random120 bits, code checks
constant-time, and wrong codes rewrite all fields with the remaining native TTL.
Successful verify keeps code until verify-new -> hash6 -> password write -> code
delete. Plaintexts/codes use redacted formatting. No user-reset table, user-session
revocation, provider hash-upgrade or account activation restriction is introduced.
Public routes retain global second-factor guard; native internal lookup is
unguarded and does not consume even an exhausted code.

Provider mail uses native provider/password-reset templates and providerPwResetUrl,
default template locale independent of user locale, branding/HTML escaping and
ProviderPasswordReset purpose/key/code headers. Existing SES/SMTP delivery is
awaited. The live Go begin201 plus readable native-format code proves the existing
synthetic SES transport completed; focused MIME/template/escaping and delivery
ordering tests complement that witness, not a claim about production delivery.

Six exact native/Go controls: unknown account403, missing internal code404,
missing verification code403, pending409 with no media header/Retry-After,
same-password409, and wrong-code403. Native reference performs three wrong tries
then rejects the correct exhausted code; Go rejects that exhausted native code
and internal lookup retains the identical key/value. Go wrong-code attempt keeps
native-created key/value and later completion succeeds. Completed code lookup404
matches native. Distinct actors stay within the configured five-operation provider
buckets; no rate policy or original fixture settings changed.

Actual Haskell -> Go -> Haskell: Go consumes a native-created code, writes a
six-character Reset6 password, accepts it and rejects its predecessor; restarted
native accepts that Go-reset password and rejects the predecessor. Go also creates
another code through public begin, and restarted Haskell reads the identical pair,
completes it and logs in with the resulting Native6 password. These are real
native provider rows/code CQL and signed provider cookies, not copied specifications.

All22 original compiled MLS/connection executions pass with exact names matching
previous provider-auth XML and no failure/error/skip. Existing provider-profile14,
provider-auth6, native Nginz valid405/forged401, optimistic-Dynamo/STOMP service/
bot/Galley single-delivery and client/prekey/MLS/connection switchback controls
pass on the same executable. All25 managed exits0, runner0/cleanup0 and independent
owned containers/networks/volumes inventory empty.

Retain failures: initial0625 SDK unused-import vet error; fd564 native setup fails
on overlong generated actor email (not captured before assertion);84d4 rerun
intentionally canceled with SIGINT during dependency wait once overlong localparts
were identified (runner130, no backend child executions, cleanup0); c142 captures
another overlong missing-email sentinel's native400 instead of intended403/404
and the native pending409/no Content-Type. Shorten synthetic addresses and fix
only Go pending media. The fresh3c backend gets native reset/exhaustion/deletion
and Go auth/profile controls, then fails because new reset_cases helper is
shadowed by the older user-reset list. Rename only new helper; AST confirms no
assignment shadows it. Existing response comparator and original assertions are
unchanged. Failed executed backends have23 or25 zero managed exits, runner1/
cleanup0/empty namespace. Aborted setup is explicitly not passing evidence.

Same-author bounded review by codex under explicit user delegation recorded in
docs/go-migration/plans/brig.md: reviewed Provider.API/VerificationCodeSubsystem/
Cassandra/mail sources, code/key/scope/TTL/retry and partial-failure order, guard
and cookie policy, exact raw HTTP/native switchback/XML/artifact reuse/cleanup.
This is not independent review. Unchanged Haskell retains existing CI; no native
full-repository build was run or converted into a pass.

Inherited concerns: provider reset exposes account existence; delivery failure
leaves its reset code/throttle pending; provider password resets do not invalidate
already-issued cookies. Future anti-enumeration/revocation/delivery-recovery changes
need separately reviewed contracts/security policy. This migration preserves them.

This bounded proof is not whole Brig: provider registration/activation/email/
delete/public service/bot APIs and whole535/eight-Go/all-entity three storage modes/
loaded operations/image/default-preserving selector/legal gates remain open.
Haskell remains supported/default. No push, deployment or compatibility promotion.
Raw artifacts/controller are retained under /home/operator/.cache/wire-migration/
brig-20261010/provider-reset-observer-backend, provider-reset-media-sdk-check and
provider-reset-observer-backend-controller.py. That controller checks clean source
and exact artifact/reuse inputs, and invokes the owned observer with the existing
flags plus --provider-reset-contract.
