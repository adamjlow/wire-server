# Local connection updates, notifications and native state readback

Qualified source `1af9e686df26280badd8a770552be2e3064dc5b6`, Brig SHA256
`e8fed1e20e9c59c11c43b05874c580ab7c96f2b4636293e3f59c82c27f09bfa3`.
Full pinned Go1.26.9 format/dependency/vet/unit/race/security/license/build and
static normal unit checks pass. Complete source-backed local state transitions
include accept/ignore/cancel/block/unblock/no-op, missing-row and legalhold history
admission, and native Sent/Sent acceptance returning the reverse row. Peer effects
use the existing native maximum version, exact Galley paths, optional connection
headers and bounded retry behavior. Deterministic MLS ID follows native SHA256
namespace/textual qualified-user/UUID5/owner-bit algorithm.

Focused tests cover missing-legalhold history rejection, no-op with no effects,
Proteus block then established MLS block before durable write/notification,
unblock counterpart updates, cancellation of both rows, Sent/Sent reverse result,
peer failure preventing writes, and absent versus explicitly empty event origins.
Creation now preserves an explicitly empty required Z-Connection as native does.

Actual Docker witness: four native/Go controls match exact status/media/JSON:
invalid transition403, unchanged204, missing connection403, legalhold request403.
Go creates actual directed rows/Galley conversation, then seven update responses
are ignored200, accepted200, unchanged204, blocked200, accepted200 for requested
sent, blocked200 and accepted200. Both final rows are accepted. Go Gundeck returns
four writer and five recipient user.connection notifications with expected
statuses and recipient IDs. Delivery is verified through the HTTP notification
API, not an in-memory fake. Native restarts on unchanged stores, reads identical
final row/timestamp/conversation fields and accepts existing connection200.
Prior11reads/5opaque cursor pages/native-Go-cursor replay and signed/raw MLS/
client/prekey switchbacks also pass.

All14 unchanged compiled original MLS cases pass with strict expected-name XML14,
no failure/error/skip; SHA256 `049c40f37742ca28b505c70290f6db22f5fb6ac53d88fcdbd022da20a7400111`.
All25 managed exits0/runner0/cleanup0/no force; independent owned project audit
empty. First SDK attempt source1bfce37f3 failed at vet on duplicate variable
selection, before backend execution. Failed receipt/log retained; only candidate
was corrected to reuse existing selection; no native/oracle assertion change.
Owned SDK-cache maintenance removed1834 regular non-symlink UID/GID65534 cache
entries older than1hour (804491276bytes) before builds; retained artifacts untouched.

Same-author scoped review under explicit user delegation accepts this increment,
not whole Brig/connections. Incoming/outgoing remote actions, original connection
suites, actual positive MLS membership/derived-ID proof, cancellation backend
witness, team/LH/app/limit and parser/error variants remain open. Notification
witness covers tested recipient/status delivery, not every original websocket
assertion. Whole535/eight-Go/all-entity3mode/operations/image/default selector/
legal/security approval remains required. Haskell supported/default; local commits.
