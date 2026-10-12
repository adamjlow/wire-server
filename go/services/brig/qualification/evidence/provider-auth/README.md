# Provider authentication qualification

Production `98ec9f19b61b557c69523f83d487bcea671c5b36`, final observer
`ee7bc4bca` (full revision in result.json), executable SHA256
`899523f667e56ec540e3111febaa8c0e013242dda448a468b9c9fc1551d6fe20`.
Go1.26.9 pinned SDK and original native runner identities/archives are in receipts.
Full format/tidy/verify/vet/unit/race/build/vulnerability/license checks and static
normal runner units pass. Observer/document-only commits reuse exact unchanged
production/tests/dependency/compiler inputs; actual artifact build source retained.

Implements POST /provider/login and PUT /provider/password with native provider
email keys, password-only CQL, LocalQuorum/read retry1/write retry5, provider rate
namespace and existing bounded password workers. Both changed passwords use
native six-character minimum. Native order is old verify -> new verify -> hash6
-> write. Verification ignores suggested hash updates; provider cookies are not
stored as user sessions or revoked on change. Preserve the global second-factor
guard and required gateway Z-Type=provider/Z-Provider on protected routes.

Six exact native/Go negative controls cover wrong password, missing email key,
unactivated login, unactivated wrong-old password, same-password409/native reset
message, and wrong gateway token type. Login/change/new-login/old-rejection match
native status/media/body. Distinct native-created actors respect the configured
five-operation provider buckets; no rate/security policy changed.

Every successful native/Go login inspects zprovider identity/type/version,
configured60-second TTL (native one-second cached Now accounted for), matching
UTC cookie expiry, /provider path, HttpOnly, configured insecure flag and absence
of domain/MaxAge/SameSite. Real native Nginz GET /v19/provider/assets returns405
for valid native and Go-issued tokens, and401 for forged signatures. This is a
no-write request reaching native Cargohold. Actual Haskell -> Go -> Haskell on
unchanged Cassandra proves native-written Nextp6 replaced by Go-written GoNew6,
then fresh native login accepts GoNew6 and rejects Nextp6.

All22 original compiled MLS/connection executions pass with exact unchanged names
matching previous provider-profile XML; no failures/errors/skips. The same binary
passes prior14 provider profile controls/native readback, optimistic-Dynamo/STOMP
service/bot/Galley single-delivery and client/prekey/MLS/connection switchback
controls. All25 managed exits0, runner0/cleanup0; independent namespace inventory
contains no containers/networks/volumes.

Retain both failed observer attempts: ffe97767d assumed uncached native Now for
the new TTL assertion and failed before Go startup; 40feca9d3 captured exactly
one-second native clock offset, then targeted Federator8098 instead of Nginz8080,
so valid/forged tokens both404. Correct only new task-owned observation assumptions,
not the existing comparator, original assertions, candidate or Haskell. Each
failed run has all23 managed exits0/runner1/cleanup0 and empty namespace.

Same-author bounded review by codex under the explicit user delegation recorded
in docs/go-migration/plans/brig.md: checked native Provider.API and auth/password/
ZAuth contracts, store query/encoding/retry choices, verification/write ordering,
error and cookie boundaries, artifact reuse and raw response/XML/cleanup evidence.
This is not independent review. Inherited provider cookies remain usable until
expiry after password changes; record this concern without changing policy.

This proves these two provider endpoints, not whole Brig: remaining provider
registration/activation/email/reset/deletion, public service/bot families and the
whole535/eight-Go/all-entity three-mode/operations/image/selector gates remain open.
Haskell/defaults/compatibility gates are unchanged. No push or deployment.
Raw backend/artifacts are retained outside repo under
/home/operator/.cache/wire-migration/brig-20261010/provider-auth-gateway-backend
and provider-auth-sdk-check. Reproduce with the retained provider-auth-gateway-
backend-controller.py, which verifies clean source/artifact/allowed reuse changes
and invokes the existing owned observer plus --provider-auth-contract.
