# Bound service deletion with typed Galley adapters

Source `a73fd44e7d69da05d7b96bbf4f6db4ee0057190f`, service
`b8256ee1c7d76bfc3f156aa9b6f874ee88f5252599482a1f5534352dbd0101d2`:
[pinned Go checks](sdk/go-check.log), [static original-runner units](sdk/runtime-units.log)
and [exact build receipt](sdk/build-receipt.json) pass. Exact tagged stores
`6087a61b3263cb30e9fd4b5faa5b917c5246ae197460f66c5f99cfe0cd87f014`
execute against unchanged native-created schemas: [receipt](stores/result.json),
[assertions](stores/test.stdout), [native schema output](stores/native-schema.stdout).
Store execution2.75sec passes new and prior controls. All controller/schema/unit/
test/cleanup exits0; [independent resource audit](cleanup.json) is empty.

Startup now binds service.delete to actual configured user/bot stores, direct
client/prekey/lock deletion, Galley bot/service calls and final CQL service/
whitelist/index cleanup. Native effect order and per-page concurrency16 remain.
No successful stub or native process/library embedding is used. Existing queue
listeners acknowledge only callback success; failures leave replay to the broker.

DELETE/i/bots sends conversation/bot and Z-User=bot, with no Z-Connection for
queued deletion. Only200/404 are admitted;404 ignores body,200 validates the
native Event envelope and tagged payload before account/client effects.
DELETE/i/services sends id/provider, accepts any2xx and uses existing native
three-retry transport/5xx policy. Focused actual HTTP controls check request
shape, header admission, valid event, malformed/empty200 and rejected202/204;
service disconnect proves503 retry then202 admission. No peer body/error leaks.

Decoder branches represent all23 native Event tags, including V2 own-conversation
members/access-role/MLS fields, optional/null/default semantics, exact bounded
JSON scientific integers and canonical padded base64. All14 unchanged native
Event golden files are accepted; three native conversation inputs receive an
explicit V9-to-V2 access-role/legacy-MLS projection without modifying originals.
These are candidate unit inputs, not execution of upstream Haskell assertions.
Finite negatives exercise missing envelope/member-leave fields, enums, array
nullability, Word64/Int32 bounds, fractions/huge exponents and base64 line breaks/
noncanonical padding. Native source: Data.Schema.optField treats null as absent;
Alternative defaults handle malformed legacy/default fields. Native History
parseOnly accepts a valid prefix without requiring endOfInput; candidate preserves
that permissiveness. Body size stays bounded at4MiB.

Reference base64 behavior was checked against primary
[decoder source](https://raw.githubusercontent.com/haskell/base64-bytestring/master/Data/ByteString/Base64/Internal.hs).
Go's decoder ignores CR/LF even in strict mode, so explicit line-break rejection
is required alongside Strict padding-bit validation. No dependency was added.

Overfull valid tag sets now fail **after** whitelist cleanup, retaining the
service row as native unfoldTags does. Actual store controls prove the committed
whitelist phase and retained metadata, correcting the prior early-failure limit.
The native large-whitelist single-batch availability concern remains recorded;
no security policy is repaired. Static linker warnings remain in [SDK output](sdk/controller.log).
Idle owned cache pruning removed8535 regular UID65534 files older than600sec
(949550131 bytes) before building, with no live cache owner; this is maintenance.

Limits: compiled-native Event malformed/admission comparisons and actual native
Galley/broker service-deletion replay remain unproven. A branch for every event
is not proof of exhaustive JSON/date/domain/URI/parser equivalence. Earlier
URL/PEM/Unicode-version limits remain open; no unsupported exception is approved.
The current binary has no new original22/e2e run; older original22/ActiveMQ results
retain their exact older tuple. Whole535/eight-Go/all-entity operations, image and
selection gates remain incomplete. Same-author scoped review under the user's
existing delegation accepts this increment only. No push or stage/default change.
