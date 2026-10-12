# Provider service reads and queued deletion

Production `a2fb668aa052ba18f1cfd6c8dce206cc25f7be11`, observer
`b32dd9af0` (full revision in result.json), executable SHA256
`53f07fec212b10ba81a928f6b6c81ddaa2ff21ff8e26c9fbf82c1835fd3dafde`.
Fresh pinned Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license
checks and static original-runner units pass. Archive/SDK/native runner identities
recorded in receipts. Observer-only changes reuse unchanged production/tests/
dependencies/compiler inputs with actual build-source identity retained.

Implements GET/HEAD /provider/services[/sid], GET/HEAD
/providers/pid/services[/sid], GET/HEAD /services/tags and DELETE
/provider/services/sid. Provider reads expose native full definition; public
reads select only public columns and never URL/tokens/keys. Native summary-null
becomes empty string, assets/tags empty arrays, tag bits become enum-sorted sets,
RSA PEM normalizes from the parsed block, and image asset size codec is reused.
Lists include disabled rows and do not require an existing provider account;
public list uses native Consistency One, other reads LocalQuorum/retry1.
Catalog is a bare31-tag array. Existing provider/user headers and global feature
guard apply; service404 is bare JSON, success200 has native charset media.

Public DELETE follows password verification in provider rate namespace -> service
lookup -> logged LocalQuorum/retry5 disable -> native service.delete enqueue ->
empty202. No extra account activation lookup or hash upgrade; queue failure
retains disabled state. Existing owned STOMP/SQS producer and validated worker
perform bot/client/index/Galley cleanup. Parameterized queries, existing password
worker/rate/context/peer bounds, redacted definitions and generic errors. Focused
units cover public projection without secret columns, null/default/tag order,
private required fields, scoped guards and delete auth/lookup/disable/enqueue
failure ordering. No new dependency/schema/security policy change.

Fourteen actual native/Go response comparisons match exact status/media/body-or-
JSON: private list/get/HEAD, public list/get/HEAD, all-tag catalog, missing private/
public service, unknown-provider empty private/public lists, wrong provider token
type, wrong delete password and valid-password missing-service404. Metadata comes
from native-schema fixtures and real native-created providers; captured tokens
are synthetic and RSA key is public example material. No global normalization
or original assertion changes.

Real public DELETE against native and Go returns202 and produces one broker
delivery (dispatch delta equals existing queue size+1), preventing bad200 Galley
decode hidden by404 replay. Actual stored service/index/whitelist/Galley connection,
bot associations, clients/prekeys and Galley membership are removed; bot user
status becomes deleted while native identity/MLS/property rows remain. Native
switchback reads identical Go-cleaned raw state, service404 and empty list. This
contrasts with provider-account deletion's separately retained native bot state;
no account/service cleanup behavior is silently unified.

All24 unchanged original Go tests pass (Provider reset/email2 plus MLS/connection22),
native Provider2 pass, exact XML names/no failure/error/skip. Dynamic Brig hashes
prove candidate selection. Existing provider enrollment/auth/reset/deletion,
Nginz/optimistic-Dynamo/STOMP/Galley/client/connection witnesses pass on this
artifact. All25 managed exits0, original subprocesses0, runner0/cleanup0 and
independent owned containers/networks/volumes empty. No failed attempts in this
bounded service-read/delete increment; earlier failures remain recorded.

Same-author bounded review by codex under explicit user delegation recorded in
docs/go-migration/plans/brig.md: native schemas/projections/consistency/headers/
error/PEM/tag/default/queue order, public secret isolation, real single-delivery
cleanup/native readback/original XML/artifact reuse/cleanup evidence. Not independent
review. Unchanged Haskell retains existing CI; no local full Haskell build was
run or claimed passing. Existing provider orphan/cookie/code and service deletion
identity/MLS/property retention concerns remain documented without repair.

Remaining public service create/update/connection/search/whitelist/bot APIs and
other whole Brig surfaces, full535/eight-Go/all-entity three modes/loaded operations/
image/default selector/legal gates remain open. Haskell supported/default; no
push/deployment/compatibility promotion. Raw controller/artifacts under
/home/operator/.cache/wire-migration/brig-20261010/provider-services-backend,
provider-services-sdk-check and provider-services-backend-controller.py, executing
--provider-service-contract with the existing owned observer/original-test flags.
