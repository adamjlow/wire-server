# Incoming federation account deletion

Source `cf35274768d7df12577431e10309797428484353`, service
`f62dafc40434ed949704d141268aaf77fc608291b9d92eb0dc16ba0888118bde`:
[full pinned quality/security/license checks](sdk/go-check.log) and
[original-runner static units](sdk/runtime-units.log) pass.
[Receipt](sdk/build-receipt.json) identifies compiler SDK/toolchain/archive
manifest and service/unit/tagged artifacts. No additional dependencies.

Actual native-schema tagged store
`0ff22b0b8423b60e37ea2f5118d46bae14636c7cda24bc0eda8e2ffc6361bcca`
[passes](stores/result.json). Native schema utility and candidate exit0; existing
three-selector user/account/group/client controls remain green. New17-local
remote cases cover all raw relation values0..11, duplicate accepted local input,
preserved requested order, exact matching remote-user/domain deletion and
retention of another remote user and another domain for every local. Invalid
stored relation12 is rejected. Native schema/image identities are in the receipt.
This proves actual Cassandra operations, not paired native HTTP or end-to-end
delivery from the other backend.

POST/federation/on-user-deleted-connections is attached to real stores and the
existing asynchronous push transport. Unit HTTP controls verify method/origin,
required exact keys, invalid UUID/empty/oversized recipients before effects;
then lookup, accepted-contact direct qualified remote user.delete and deletion
order, failure prefix, removal including nonaccepted contacts, and200 EmptyResponse
object/content type. No public DELETE/self endpoint is inferred from this handler.

Reference: Brig.API.Federation.onUserDeleted, Brig.Data.Connection remote status
lookup/delete and federation EmptyResponse/payload schemas. Preserve16 workers,
LocalQuorum reads with one retry, per-local right_user IN [deleted] projection,
raw accepted filtering, input ordering/duplicates and non-retried row deletes.
Reuse native128 recipient push chunking. Parameterized CQL, bounded recipients
and concurrency, context cancellation and scrubbed errors preserve boundaries.
The origin header relies on the same trusted Federator/gateway as native; do not
expose that internal trust boundary to untrusted callers. Whole JSON allocation
retains native behavior; gateway request-body limits remain an inherited concern,
not a security-policy repair in this migration.

[Cleanup](cleanup.json) confirms terminal SDK/store controllers and independently
empty owned containers/networks/volumes. [Cache maintenance](cache-maintenance.json)
removed only idle owned compiler-cache entries, not retained evidence/artifacts.
Same-author review under the user's existing delegation accepts this increment;
no independent or whole-service approval is claimed. Public verification/mail,
internal replay and complete deletion pipeline/retained original22 retry remain.
Final535/eight-Go, operations, all-entity three-mode switchback and image/selector
requirements and service gates remain unchanged.
