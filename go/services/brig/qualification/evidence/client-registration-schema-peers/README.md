# Client registration schema and actual peer transports

Production source `5f79e98a199a206afac5f32d4552bcda056f949b` passes
full pinned Go1.26.9 SDK checks and the normal static unit executable in
the unchanged original runner. Receipt identifies service, normal-unit
and tagged-store executables; tagged stores compile only. Both containers
exit0/network removal0 and exact-name independent cleanup audits empty.
Existing static glibc/OpenSSL linker warnings retained. Native Haskell
builds and original compiled Client cases were not run here.

Native sources: Wire.API.User.Client.newClientSchema/capabilitiesSchema;
Wire.ClientSubsystem.ReAuthPolicy/Interpreter; GalleyAPIAccess.Rpc.newClient;
Wire.API.UserEvent's ClientAdded schema; NotificationSubsystem.Interpreter
toV2Push; Push.V2 schema and existing GundeckAPIAccess transport.

New decoder requires prekeys/lastkey/type, last ID65535 and typed optional
class/cookie/model/password6/code/capability/MLS fields. It reuses the existing
first-key/decimal-preserving JSON and key/capability/MLS decoder. Body
validation does not perform the bundle checks prematurely: the native core
validates bundles after admission/reauthentication/capacity. Missing/null
capabilities remain nullable for CQL; explicit empty lists remain present.
V6 requests use capability arrays, unlike V6 response objects; V7 rejects
consumable capability and V8 accepts it. Legalhold parses here and remains
subject to later public policy/internal augmentation. An explicit internal
reauth policy can override native default count>0/not-upsert.

Actual HTTP unit witness checks Galley POST/i/clients/a, Z-User and Request-Id,
empty body and2xx acceptance. Actual synchronous Gundeck POST/i/push/v2
checks chunkedJSON, no Z-User, Request-Id, RouteAny/all-client recipient,
origin/connection, user.client-add and native V6 client serialization even
with both capabilities stored. Real HTTP503 propagates with no retry rather
than entering the async session/property worker. Focused decoder negatives
and internal policy controls also pass. These are local real HTTP controls,
not native backend captures or full addClient state/effect qualification.

Public registration is still unwired: actual account/login-code admission,
legalhold policy, configured limit/reauth binding, consumable setup, old-client
cookie revoke→native deletion enqueue→client/prekey/Dynamo lock deletion,
Galley→synchronous event→new-client email require composition. The existing
SQS internal queue already handles user.delete; next extend its same response
MD5/retry primitive to client.delete without changing user deletion. STOMP
and consumer lifecycle remain whole-service requirements. Then run original
Client cases and actual native-Go-native registration/deletion state witness.

Same-author slice review under user's explicit delegated self-approval
accepts the stated schema/peer boundaries; no independent or whole-service
approval claimed. Preserve inherited registration reauth exemptions and
nontransactional storage/peer ordering; no security-policy/Haskell/oracle/
schema/default change. Whole535/eight-Go/ops/all-entity/image/selector/review
qualification remains incomplete and Brig unavailable.
