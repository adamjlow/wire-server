# Provider enrollment and email qualification

Production `688ac8404c8aff8115ff77f15f79d337e8d433d2`, observer
`1fd59542c4e42fb0546bae2aa18a21ea98632a33`, executable SHA256
`3c321e5bce5363c0f46edc48e7addce812f5f37e218789a629e39c75114b7dbd`.
Pinned Go1.26.9 SDK/original native runner/archive identities and compiler source
are recorded. Full format/tidy/verify/vet/unit/race/build/vulnerability/license
checks and static original-runner normal units pass. Observer/docs-only commits
reuse exact unchanged production/test/dependency/compiler inputs; build source
remains688, not the later observer revision.

Implements POST /provider/register, GET/HEAD /provider/activate?key&code,
GET/HEAD /i/provider/activation-code?email and PUT /provider/email. Native name/
email/HTTPS/description/optional-password bounds; required forwarded IP and
per-IP hash6 for supplied passwords. Missing/null password generates12 random
bytes in standard base64 (16 characters), hash8 and returns it once; supplied
password response omits it. Registration inserts hidden account before scope2
code/throttle/mail; scope2 retains three attempts/24h TTL. Mail uses native
provider activation/update/approval-confirm templates, default template locale,
providerActivationUrl/homeUrl, branding and HTML escaping, native purposes and
existing awaited SES/SMTP transport. Provider gateway type/id and global feature
guards are preserved; email update uses native placeholder name and no added
account-existence gate.

Activation preserves code for replay204. First activation validates new key
owner, writes logged LocalQuorum key/email batch, then awaits approval mail.
Email changes remove old scope3 reset code before key collision/batch, delete
old normalized key and insert new key/original email. Reads retry1/writes retry5;
no schema/LWT/transaction or Haskell changes. Provider code throttle uses native
Retry-After/JSON/no Content-Type (source-guided regression); previous pending
reset response stays identical. Focused tests cover registration schema/generated
credential shape and write/mail/throttle-lookup failure ordering, activation
initial/replay/change/collision/approval/batch failure ordering, three-attempt
remaining-TTL exhaustion, provider mail and throttle media.

Original compiled tests Provider.testProviderPasswordReset and
Provider.testProviderPasswordResetAfterEmailUpdate execute unchanged against
native Brig and Go Brig. Both native2 and Go2 XMLs have exact same names, no
failures/errors/skips. Runtime dist/brig bytes are hashed before each launch and
match native reference/candidate respectively: dynamic modified-backend tests
cannot silently select Haskell for the Go phase. Their original configuration
changes and assertions remain intact; no copied request specification replaces
execution. Tests prove real registration/activation, provider reset, email update,
old reset-code invalidation, old-email rejection and new-email/reset login.

Existing22 original compiled MLS/connection executions retain exact names and
pass with no failures/errors/skips, giving24 original Go executions on this
artifact. Supplemental native/Go controls match exact missing-IP400, active-email
registration409, missing activation-code404, self-email update409, wrong code403
and activation replay204. Native200 activation email schema/media is preserved
with actor-specific email. Supplied/generated Go registration response schemas
and media match independently captured native responses; random IDs/credentials
are validated rather than equated across independent actors. This adds no global
normalization or response-comparator change.

Actual native -> Go -> native state witness: native creates a pending provider
and scope2 code; Go rejects one wrong code, activates it, and replay is204, which
native later reproduces. Go also creates and activates two new accounts (supplied
and generated credentials), returns hidden404 before activation, and exposes
exact name/email/url/description. Restarted Haskell reads identical Go-created
profiles and key/code pairs, and accepts both Go-stored credentials with real
signed provider cookies/native Nginz acceptance. Prior provider profile/auth/reset,
optimistic-Dynamo/STOMP service/bot/Galley/client/MLS/connection switchback controls
also pass. All25 managed exits0, original subprocesses0, runner0/cleanup0 and
independent owned containers/networks/volumes inventory empty. Dynamic original
processes are owned by the original runner; this is not final loaded-shutdown proof.

Retain observer93a31 aborted run: read-only review found a new generated-response
variable would be overwritten by existing MLS material subprocess variable.
SIGINT stopped disposable dependency initialization before backend/original test
execution; runner130/cleanup0/independent namespace empty. Namespace new enrollment
variables with token NAME-only replacement; existing JSON keys/strings, assertions
and comparator unchanged. Retain raw aborted record as failure, not passing tests.

Same-author bounded review by codex under explicit user delegation recorded in
docs/go-migration/plans/brig.md: reviewed native Provider.API/DB/code/mail sources,
validation, original and generated credential/hash policy, effect/partial-failure
order, key updates/replay/code scope isolation, real original XML/dynamic binary
selection and switchback/artifact/cleanup evidence. This is not independent review.
Unchanged Haskell retains existing CI; no local full-repository Haskell build
was run or claimed passing.

Inherited concerns: concurrent/duplicate unactivated registration and mail/
throttle failures may leave hidden provider rows/codes; initial approval-mail
failure leaves activation committed; changed-email reset-code deletion precedes
new-key collision failure; provider cookies remain valid until expiry after
credential changes. Preserve these rather than change contracts/security policy.

Remaining: provider deletion/public service/bot APIs, remaining whole Brig
surface and full535/eight-Go/all-entity three-mode/loaded operations/image/default-
preserving selector/legal gates. Haskell remains supported/default; no push,
deployment or compatibility promotion. Raw reproducible controller/artifacts:
/home/operator/.cache/wire-migration/brig-20261010/provider-registration-scoped-
backend, provider-registration-sdk-check and provider-registration-scoped-backend-
controller.py, using --provider-registration-contract in the owned observer.
