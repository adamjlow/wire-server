# Internal SQS deletion listener and durable replay

Source `a14fe1243bbc03697ba46da6ea8507639a60f882`, service
`def0fef2b8a58855000e2f7cd92d18b394ec75c8cabfa1e8edc99300e0f8a9a1`:
[full pinned Go quality/security/license checks](sdk/go-check.log) and
[static original-runner units](sdk/runtime-units.log) pass.
[Receipt](sdk/build-receipt.json) records exact source/compiler/artifacts.
Earlier production revision c0e7f4b86 is retained in initial-sdk; adding only the
owned observer produced identical service/unit/tagged hashes on the new full build.

Listener preserves native SQS long poll20/max10, concurrent callback cancellation,
malformed/missing-body retention, callback-success-before-DeleteMessage,60-second
event deadline and user deletion throttle/default100ms. User deletion looks up
accounts without status filtering and replays cleanup even for tombstones; missing
accounts still wait the throttle. Legacy client object/id and required nullable
connection are supported. Client cleanup removes Galley then Gundeck with exact
200/204/404 admission and replayable retries, then sends the native client-remove
event including optional origin_connection. No client storage removal is inferred
from that queued notification; its producer already removes client/prekey rows.

Signed HTTP units exercise acknowledgement and callback/delete failures, invalid
events, exhausted/canceled delay, listener cancellation/join, exact peer ordering,
legacy decoding, removal-event shape and status/retry admission. Shared batch
extraction retains SES behavior; all existing SES controls remain green. Queue
bodies/receipt handles/credentials are not logged. No dependencies added.

[Actual paired evidence](paired/paired-reads.json) proves native queue cleanup,
then a Haskell-created account and property enqueued only after native ownership
ends. Go consumes it, writes the native deletion tombstone, clears the property
and acknowledges the durable message. Restarted Haskell reads that same state.
Visible/in-flight queue counts are0 in all three observations. Existing paired
admission/effect and client/prekey/raw+signed MLS durable controls also pass.

All [22 unchanged original MLS/connection executions](paired/mls-upload-original.xml)
pass with no failures/errors/skips: [log](paired/mls-upload-original.log),
[backend receipt](paired/result.json). This is Go Brig/current patched Gundeck
with Haskell remainder, not final eight-Go535. All25 managed exits0, runner/
cleanup0 and independently empty owned resources: [cleanup](cleanup.json).

STOMP transport and provider/service.delete cleanup remain unfinished. Parsed
service-delete messages currently fail without acknowledgement; malformed messages
are retained as native does. These limitations prohibit whole-service availability.
Actual durable client queue cleanup, email-code deletion delivery, remaining APIs,
final operations/all-entity switchback and optional image/selector remain in the
finite whole-Brig acceptance matrix. Haskell/defaults/security policy unchanged.
Same-author review under the user's existing delegation accepts this increment;
no independent or whole-service approval claimed.
