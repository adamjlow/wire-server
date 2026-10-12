# Provider deletion qualification

Exact production/observer `e8dfc7b0a6539f0f1211ef673b86f9174ee391b8`, executable
SHA256 `d185210e6c313a0487ad8a7b7031ed899a580f59ab6a258d7d4a61038aad5b2f`.
Fresh pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks and static original-runner units pass. Compiler/native runner/archive
identities are recorded in receipts; no new dependencies or schema changes.

DELETE /provider requires provider gateway type/id and JSON password6..1024.
Global feature guard -> visible account lookup -> provider-password verification
(ignore upgrade status) -> full native-schema service-list decode -> sequential
Galley disconnect/metadata cleanup -> normalized email-key removal -> provider
row removal -> empty200. Reuses service row validation/index/whitelist cleanup
and actual Galley adapter. LocalQuorum/read retry1/write retry5, parameterized
queries, existing worker/rate/context/peer limits, generic errors and no secret
service fields in responses/logs. Native no-explicit-page service query is read
before mutations; failure stops in native order and retains prior effects.

This is synchronous provider deletion, not the queued service/bot cascade.
Focused tests cover activated account-before-credentials, two-service ordering,
list/disconnect/partial metadata/key/provider failure cutoffs, header/password/
feature guards and observed missing/unactivated404 response.

Actual comparison uses two native-created activated providers, each with two
native-schema services, index/whitelist entries, real Galley connections and bot
memberships, plus native-created bot users/clients/prekeys/MLS/properties.
Four exact negative responses match: wrong token type403 empty, missing provider
404 bare JSON, unactivated provider404 bare JSON, wrong password403. Both native
and Go DELETE return200 empty/no media. Metadata/index/whitelist/Galley connection
counts become0 while bot/user/association/client/prekey/MLS/property counts and
bot membership remain exactly as native. Profile404, login403 and repeat-delete
404 match. Restarted native sees Go provider/key absent and exact same raw
metadata/bot state on unchanged stores.

All24 unchanged original Go executions pass: Provider reset/email2 and previous
MLS/connection22. Native provider2 also pass; strict XML names match with no
failure/error/skip. Dynamic original Brig byte hashes prove actual candidate
selection. Prior enrollment/auth/reset/profile/Nginz/optimistic-Dynamo/STOMP/
service-bot/Galley/client/connection switchback controls pass on this executable.
All25 managed exits0, original processes0, runner0/cleanup0 and independently
empty owned container/network/volume namespace. This is not final loaded-shutdown
or intended eight-Go qualification.

Retain first sourcef734/observer2aca failed native capture: inferred InvalidProvider
403 from the helper/type name, but native helper1094 throws ProviderNotFound404.
Correct only Go404 media/message and new observer status assumption using raw
native responses/helper source; original assertions/comparator unchanged. Native
original2 had executed successfully before that failure (its XML is retained,
even though the generic e2e flag is only set later in the Go MLS stage). No Go
backend test was reached. All23 managed exits0/runner1/cleanup0/independent empty.

Same-author bounded review by codex under explicit user delegation recorded in
docs/go-migration/plans/brig.md: native handler/helper/CQL/RPC order, reuse of full
row validator and cleanup, auth/rate/error/privacy boundaries, raw two-service
state/native switchback/original XML/hash/cleanup evidence. Not independent review.
Unchanged Haskell retains existing CI; local full Haskell builds were not run or
claimed passing.

Serious inherited concern: provider deletion leaves bots, bot clients/prekeys/
properties, service associations and conversation memberships even after removing
provider/service metadata and connections; issued provider cookies and verification
codes are not revoked/deleted. The comparison proves this behavior, rather than
repairing Haskell or silently changing security policy. Any cascade/revocation
improvement requires a separately reviewed contract/security change.

Remaining public service/bot APIs and other whole Brig surfaces, full535/eight-Go/
all-entity three modes/operations/image/default selector/legal gates remain open.
Haskell supported/default; no push/deployment/compatibility promotion. Raw artifacts
and controller under /home/operator/.cache/wire-migration/brig-20261010/provider-
deletion-corrected-backend, provider-deletion-corrected-sdk-check and provider-
deletion-corrected-backend-controller.py; it executes --provider-deletion-contract
plus the existing owned observer/original-test flags.
