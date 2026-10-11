# Real Galley membership deletion and connection cleanup

At observer/fixture source `27b4d06390d7d555ff0826f5a1d1d3501bda5ffc`, service
`b8256ee1c7d76bfc3f156aa9b6f874ee88f5252599482a1f5534352dbd0101d2`
and tagged fixture `40a0477ca0dc63b9259ad2ba59926a4ec976c0dde0cef763da279ec48773b9e5`
pass the owned mixed-backend comparison. [Production reuse](production-reuse.json)
records exact unchanged production/normal-unit/dependency/compiler/native archive
inputs against qualifieda73; no new full Go quality run is implied.
New helper [format/tagged vet/static build](tagged-build.json) passes with
[linker output retained](tagged-build.log).

Native Galley HTTP creates real Proteus conversations, disabled service
connections and bot members. The helper attaches source-defined bot fields and
synthetic association/team markers to native-created account/client/prekey/MLS/
property state; this is not execution of Brig's public provider/bot creation API.
Disabled connections avoid outbound provider webhook traffic. Actual conversations
and users use PostgreSQL; service metadata/connections and clients use Cassandra.

[Observations](paired-reads.json) retain a real native DELETE/i/bots200
`conversation.member-leave` event, including qualified envelope and removed
qualified/legacy user IDs. That probe member is re-added before queue work.
Native/Go queued cleanup changes actual membership from present to absent and
Galley stored connection count1→0, alongside the already-established Brig bot,
client/prekey, metadata/index/whitelist cleanup and retained identity/type/MLS/
property row witnesses. Native HTTP reads the deleted status after Go stops;
independent raw state exactly matches the Go-cleaned state. Native also consumes
Go-created durable work after Go ownership ends; both implementations ACK replay.

The broker sees exactly the expected dispatch increases:

| Operation | Dispatch delta |
| --- | ---: |
| Native queued service cleanup | 1 |
| Go consumes pending native user and service work | 2 |
| Go service replay | 1 |
| Native consumes Go-created service work | 1 |
| Native service replay | 1 |

QueueSize/InFlightCount are0, with the expected single new enqueue per service
operation. Go acceptance of the live200 path is **inferred** from real membership
present before/removal after, native Galley source returning200 for that update,
and exactly one delivery of the service item. A rejected200 decoder followed by
404 replay after Galley committed the removal would require redelivery and fail
this control. The native200 response body is directly retained; no transparent
proxy or reference implementation changes were introduced.

All [22 unchanged original executions](mls-upload-original.xml) pass with exact
names matching the preceding qualified22 and no failures/errors/skips:
[log](mls-upload-original.log). Existing client/prekey/raw+signed MLS/user queue
switchback and malformed-event redelivery controls pass. This remains Go Brig/
Gundeck and Haskell remainder, not eight Go services.
[Backend receipt](result.json) has runner/cleanup0, all25 managed exits0;
[independent audit](cleanup.json) finds no owned resources.

An [initial import build failure](retained-import-build.json) is retained with
[log](retained-import-build.log): correct old gopkg.in YAML import to the existing
go.yaml.in module, without changing dependencies. [Historical fixture dedup](historical-fixture-dedup.json)
removes only a byte-identical inactive copied executable; original identified
input and restore metadata remain. Existing isolation/disk guards and original
assertions are unchanged.

Same-author review under the user's delegation accepts this finite increment.
Positive trusted TLS, optimistic-Dynamo/STOMP, actual backend service queue across
all three user selectors, remaining Brig APIs and final535/eight-Go/all-entity/
operations/image/selector/legal gates remain incomplete. No webhook delivery,
exhaustive serialization or whole-service compatibility claim is made.
