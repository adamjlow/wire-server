# Prekey claim store/locking core — not public API qualification

Source `d11e2709c50c9e135d61e8d8d8f5792742aa5e76`, service SHA256
`36b79ae091e9b93db03eb99200102cfd520d9d08846ec9c432be9c235e1c3683`.
Full pinned Go1.26.9 format/vet/unit/race/build/module/vulnerability/license
checks pass; static normal units pass in the unchanged original runner. Tagged
stores compile only. SDK containers/network exit0 and independent exact-prefix
audit empty. No new original/native backend/CQL claim execution or full parity.

Wire.ClientStore.Cassandra.claimPrekeyImpl supplies the contract: random mode
uses one process-wide lock and reads every key at LocalQuorum/retry1, chooses
uniformly among ordinary keys unless only one key remains. Optimistic mode
uses ordered Cassandra LIMIT1. Both remove ordinary prekeys at LocalQuorum/
retry1 before returning, retain65535 for repeated use, return missing as nil.
Actual CQL operations are implemented, not yet exercised in a live claim run.

Wire.ClientStore.DynamoDB.withOptLock reads version, executes/removes the claim,
then attempts conditional version write. Failed write repeats the action up to
11 times, increments attempt callbacks10 times, and returns the final result
with exhausted callback after final failure. No invented exclusive lock or
compensation. Real signed GetItem uses ConsistentRead/key userUUID.clientID;
PutItem uses legacy Expected.version NULL or EQ and Word32 increment/wrap.
Configured random=true requires no Dynamo; nil/false uses native configured
endpoint scheme/host/port and table. No new dependency. Constructor is not yet
bound to the public server; legalhold guard/cleanup/public/federated bundle and
multi-user contracts and actual backend qualification are still required.

Finite units cover random empty/last/ordinary selection, get-action-put ordering,
success/retry/exhaustion/final result and Cassandra-error-before-version-write;
actual local HTTP signed Dynamo get/put, key/body/conditions/Word32 wrap,
unparseable version fallback, conditional rejection and malformed successful
response. Pinned Amazonka service retries reused; outer Bilge HTTP retries use
100/200/400ms. Native exception-family mapping, unusual stored Read Word32
syntax, complete typed SDK-response corners, throughput metric, exhaustion
logging/metrics binding and concurrency/missing-prekey real-state proof remain
unqualified; do not infer whole Dynamo/native parity from these units.

Primary pinned SDK sources checked for successful response parsing:
[PutItem](https://raw.githubusercontent.com/brendanhay/amazonka/b562aa3f24845e34b95748daae671860017426be/lib/services/amazonka-dynamodb/gen/Amazonka/DynamoDB/PutItem.hs),
[Response.receiveJSON](https://raw.githubusercontent.com/brendanhay/amazonka/b562aa3f24845e34b95748daae671860017426be/lib/amazonka-core/src/Amazonka/Response.hs).
Native receiveJSON requires an object even for PutItem; malformed200 payloads
must not become successful conditional writes. This was aligned after initial
core Go checks passed, then the exact changed source reran full Go/static checks.

Inherited concern: conditional-version failure can consume additional prekeys,
and exhausted best-effort locking does not guarantee exclusive claims. Preserve
it for this migration; stronger claims are separate behavior/security work.
Same-author scoped review under explicit self-approval delegation accepts core
progress only, not a complete API/SDK/storage/whole-service approval. Haskell,
protected oracle/fixtures/assertions/default/security policy unchanged. Brig
remains incomplete/unavailable; Galley follows actual whole Brig completion.
