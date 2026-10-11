# Public client deletion — core/transport proof

Production source `e9e7c84761d8786ca7c3960c26cca50652b76b1c`, service
`3a8822d748eeac0b5d1c298228e8086029b31e2eb39578fa8493ab02fde09d92`,
passes full pinned Go1.26.9 SDK checks and normal static units in the unchanged
original runner. Tagged-store binary compiled only. Both managed containers
exit0/network removal0 and independent exact-name audits empty. Existing
static glibc/OpenSSL linker warnings retained; no native Haskell rebuild.

Native sources: Wire.ClientSubsystem.Interpreter.rmClient/execDelete;
ClientStore.Cassandra.deleteImpl/removeClient/removeClientKeys;
ClientStore.DynamoDB.deleteOptLock/ddbKey; Brig.App randomPrekeyLocalLock;
CanonicalInterpreter client store wiring; Brig.InternalEvent.Types and
Brig.DeleteQueue.Interpreter enqueue; public Brig delete-client/RmClient schema.

Public DELETE/clients/:id is bound to actual current store/session manager,
reauthentication and internal SQS queue. It validates required user/connection,
normalized ID, JSON object and optional password6; lookup precedes legalhold
400, temporary skip or permanent reauthentication. Cookie-label revoke
precedes real client.delete publication, then client/prekey CQL deletes and
lock cleanup. CQL uses bound parameters/LocalQuorum/retry5. Native retains
MLS public keys. Error/partial work is propagated without compensation.
Worker Galley/Gundeck cleanup is asynchronous and remains unimplemented Go
consumer work, not fake-success cleanup in this API.

The existing user.delete publisher now delegates unchanged encoded bytes to
the same SQS primitive. client.delete is standard signed SendMessage with
client text/user UUID/required nullable connection/type, native response MD5
check and existing retry policy; no FIFO parameters. Actual local HTTP units
check null/present connection and checksum rejection; original user-delete
transport/checksum controls still pass.

Top-level randomPrekeys JustTrue uses local mode with no Dynamo DeleteItem;
absent/false binds a signed Dynamo DeleteItem to configured table and exact
client string key userUUID.clientID. Endpoint path/query discarded like
native AWSEndpoint; existing AWS credentials/region/HTTP/signing reused and
errors redact response/credentials. Actual HTTP units check signing scope,
headers, request/key,200/rejection and no local-mode request. No new module
dependency. Constructor failure cannot leak the notification workers.

Finite core controls cover missing/legalhold/permanent/temporary admission,
revoke/queue/store failure ordering. Public route units cover version routing,
ID normalization, required connection, password/null/duplicate-first object
semantics, missing/auth/legalhold errors and empty200/no Content-Type.

Qualification limitations: no actual native-stack deletion, actual CQL/Dynamo
state deletion/switchback or original Client test execution yet. Dynamo
transport currently attempts once; native SDK retry/error/default-region
comparison and any required alignment must precede optimistic-mode
qualification. Wider malformed Aeson/header/version literals remain pending.
Full addClient, deletion consumer/STOMP, last-active callbacks and original
client suite remain next work. Whole535/eight-Go/ops/all-entity3mode/image/
selector/legal/security/review gates remain incomplete and Brig unavailable.

Same-author slice review under explicit user-delegated self-approval accepts
this scoped implementation with the above open qualification requirements;
not independent/whole-service approval. Preserve inherited partial deletion
(cookie revocation can survive queue failure; event survives storage failure)
and orphan MLS rows; do not repair Haskell/security policy during migration.
