# Native service bot stores and pagination

Source `2ebed4df381df5c37ff9e50c7d24f702aade5f97`, service
`a170b4e85924f36766d0fd26b987fafabdb4d86fbef6ad4cf4c4860ff67d72d7`:
[pinned Go checks](sdk/go-check.log), [static original-runner units](sdk/runtime-units.log)
and [build receipt](sdk/build-receipt.json) pass. The exact tagged store executable
`79cf298e99fe0672e33b5f00709ea99fbbecd983069da24aa92d6c4824001318`
executes on schemas initialized by the unchanged native tools:
[store receipt](stores/result.json), [assertions](stores/test.stdout),
[schema output](stores/native-schema.stdout). All store assertions, including
prior controls, pass in2.63sec. This is real-store qualification, not API/e2e.

The fixture creates101 bots per backend. Cassandra and PostgreSQL page sizes
are100/1; native mixed pagination yields100/101/1, combining the short terminal
Cassandra page with the first full PG page before effects. All202 associations
are deleted during mixed pagination, then observed absent. CQL service_team
rows are removed. Replay is idempotent; original account provider/service/status
remain unchanged. Missing service partitions yield no bots. PG cleanup deletes
only bot_conv; CQL cleanup uses native logged service_user/service_team mutations.
Existing native user-backend selection and migration locks are reused.

The normal unit suite adds canceled-page worker joining/metadata retention and
missing/pending-account bot cleanup controls. Cancellation does not disconnect
or delete final service metadata. Missing/pending accounts still remove clients
and receive the native Deleted status but supply no service reference.

SDK/schema/test/cleanup exits are0. [Independent resource audit](cleanup.json)
confirms no owned containers, networks or volumes. No host dependencies or
Haskell/oracle changes were made. The inactive prior scalar SDK artifacts were
[losslessly archived](lossless-maintenance.json): gzip decompression hashes,
original modes and owners are retained. An idle owned compiler-cache prune removed
8416 regular UID65534 cache files older than600sec (851114368 bytes) before building;
no live process used that cache. These are maintenance measures, not test evidence.

The queue service.delete callback remains unbound. Native service metadata/index/
whitelist cleanup and typed Galley RPC adapters plus actual queue replay remain
required. Static linker warnings are retained in [SDK output](sdk/controller.log);
full native rebuild and current backend e2e were not run here. Prior original22
and ActiveMQ replay retain their older exact tuple. Same-author scoped review under
the user's existing delegation accepts this increment, not whole Brig acceptance.
Final535/eight-Go/all-entity/operations/image/default-preserving selection remain
incomplete. No stage/default changes or new push occurred.
