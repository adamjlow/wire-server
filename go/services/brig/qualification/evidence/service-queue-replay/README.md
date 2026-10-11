# Actual service/bot queue cleanup and switchback

At source `0079c6ae05f8c3fe87ed2be9cc30226b6865e899`, service
`b8256ee1c7d76bfc3f156aa9b6f874ee88f5252599482a1f5534352dbd0101d2`
and tagged fixture `a4939b68d3eab79be12fe4151fbf16a7af91008cff267c3ab09509ff7d1d555b`
pass the owned actual authenticated ActiveMQ comparison. [Production reuse](production-reuse.json)
compares every changed path with qualified a73fd44e7: only tagged helper,
observer and metadata changed. Production, normal units, dependencies, compiler
and native archives are unchanged. Full Go quality is reused; the new fixture
has [format/tagged vet/static build](tagged-build.json) with [linker output](tagged-build.log).

The helper verifies native-created account existence and attaches the native
Provider.API bot constructor fields (Active0/UserTypeBot1/activated/no expiry)
plus service references. Native HTTP creates account/client/prekey/MLS/property
state. Parameterized raw SQL attaches PG bot_conv or CQL associations according
to configuration, and source-schema CQL metadata/whitelists/literal index keys.
It never invokes candidate cleanup. This run uses **PostgreSQL users**, CQL
metadata/clients and random prekeys. Galley is real, but deliberately returns404
for the synthetic missing conversation; a successful200 membership event is not
established by this fixture.

[Observations](paired-reads.json) prove:

- Native service/bot cleanup consumes and acknowledges the actual queue event.
- Go consumes work for a native-created bot after native ownership ends.
- Go acknowledges replay when service metadata is already absent.
- After Go stops, native HTTP reads the deleted status; independent raw reads
  exactly match the Go-cleaned account, metadata and association state.
- Native consumes durable work for a Go-created actor after Go ownership ends,
  then acknowledges another metadata-absent replay.

All cleanup snapshots have service, bot associations, clients, prekeys, prefix,
all7 tag subsets and both whitelist directions absent. The user row, bot type,
name/provider/service references remain; MLS public-key and property row counts
remain1. Account status is Deleted2. These are row-count/identity witnesses,
not a claim of exhaustive byte validation of all retained values. Actual broker
DequeueCount is2 after native cleanup,5 after Go consumes native work,6 after Go
replay,9 after native consumes Go work,10 after native replay. QueueSize and
InFlightCount are0. Distinct actor IDs and enqueue-after-owner-stop are recorded.

All [22 unchanged original executions](mls-upload-original.xml) pass with no
failure/error/skip and exact names matching the preceding qualified22 run:
[log](mls-upload-original.log). Existing client/prekey/raw+signed MLS/user-property
queue switchback and malformed-event redelivery controls also pass. This is
Go Brig/Gundeck with Haskell remainder, not the eight-Go stack.
[Backend receipt](result.json) has runner/cleanup0; all25 managed process exits0;
[independent audit](cleanup.json) finds no owned containers, networks or volumes.

Two [failed fixture attempts](retained-failures.json) are retained with receipts,
raw rows and empty cleanup. The first attached CQL bot rows while native users
lived in PG; the second assumed an active actor before native bot fields were
attached. Correct the helper from actual configuration/native constructor;
no Haskell or Go production behavior was repaired and no original assertion was
relaxed. [Disk preflight](disk-preflight/controller.log) started no backend and
kept the floor. Its duplicate executable was removed only after identity matched
[the retained original](disk-preflight/duplicate-fixture.json).
[Maintenance identities](maintenance.json) record verified historical gzip
archives, inactive log compression and failed fixture archive locations.

Same-author review under the user's delegation accepts this finite increment.
Actual Galley200, positive trusted TLS, optimistic-Dynamo/STOMP and backend
service/bot queue runs across all three user selectors remain separate coverage.
Remaining entire Brig surface/final535/eight-Go/all-entity operations/image/
selector/legal gates are incomplete. Haskell/defaults and service stages remain
unchanged; this is not whole-service qualification.
