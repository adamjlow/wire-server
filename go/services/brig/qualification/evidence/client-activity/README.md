# Client activity callback and original client tests

Source `c2d03121a1be7f86402d6a03aabd3a901f1891a8`, service SHA256
`4447e62db1110dcceab9fb1fd423eb8e4f60a680f75eb3c98eecbec0d1472446`.
Full pinned Go1.26.9 formatting/vet/unit/race/build/module/vulnerability/license
checks pass. Static normal units pass in the unchanged original runner; tagged
store tests compile only. SDK/runner receipts and logs are retained. Existing
static glibc/OpenSSL linker warnings remain; no native Haskell build performed.

Native sources: Brig.API.Internal.updateClientLastActive,
Wire.API.Routes.Internal.Brig.ClientAPI,
Wire.ClientStore.Cassandra.updateLastActiveImpl/updateClientLastActiveQuery.
POST /i/clients/:uid/:client/activity rounds seconds up to the next epoch week
(604800 seconds), including exact boundaries, with nanoseconds zero. Real CQL
uses LocalQuorum/retry5 and IF EXISTS: a missing client is not recreated and
not-applied is ignored, whereas actual storage errors propagate. The existing
internal trust boundary is retained; no public auth policy change introduced.
Unit checks cover epoch/week/negative-second boundaries, normalized client IDs,
invalid captures before storage, empty200/no Content-Type and storage failure.

Four unchanged compiled original Haskell cases pass strict exact-name/count
XML checks with no failures/errors/skips: Client.testClientLastActive,
Client.testCreateClientWithCapabilities,
Client.testUpdateClientWithConsumableNotificationsCapability,
Client.testGetClientCapabilitiesV7. XML SHA256
`d5adc8ed383f809c3abd3c1108a182d853fa9a6ea813eea4c163d7253f91ede2`.
Last-active exercises actual Go Gundeck -> Go Brig callback -> Cassandra ->
Go client GET; the three capability cases exercise real registration/update
and current/v7/v6 reads. Original17 paired controls remain passing.

Backend A uses identified Go Brig/Gundeck; other peers/backend B are native,
Nginz native, dynamic Brig Go. All25 managed processes exit0; original process,
runner and cleanup exit0; independent project audit empty; no forced stop.
This is two-Go scoped evidence, not final eight-Go/whole535 qualification.
Original offline-client test still needs connections/federation. Missing-client
CQL/not-applied, native callback parser/error literal equivalence, actual client
registration/deletion durable switchback and new-device mail delivery are not
new live claims from these four cases. Whole Brig remains incomplete/unavailable.

User-delegated same-author scoped review accepts the source-aligned callback
and finite original tests; not independent or whole-service approval. Protected
Haskell/oracle/assertions/fixtures/rates/deadlines/defaults remain unchanged.
