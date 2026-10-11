# Client registration core — scoped proof

Production source `f17fbcdf19267ad989d3893bdfd9dece1146913a` passes the
full pinned Go1.26.9 SDK checks and normal static unit executable in the
unchanged original runner image. Receipt identifies service, unit and
tagged-store binary hashes; tagged-store tests were compiled, not executed.
Both managed container invocations exited0, owned network removal0 and
independent exact-name container/network audits are empty. Go format/tidy/
verify/vet/unit/race/build/vulnerability/license checks pass. Existing static
glibc/OpenSSL linker warnings remain in the unmodified log; native Haskell
builds were not run.

Native source: Wire.ClientSubsystem.Interpreter.upsertClient and
Wire.ClientStore.Cassandra.upsertImpl/insertClient; timestamp truncation from
Data.Json.Util.toUTCTimeMillis. The core implements same-type counting and
ID-upsert, native default reauthentication before capacity, permanent limit,
time before all-prekey validation, last-key-first insertion, client-row write
then enum-ordered MLS insertion, temporary/legalhold replacement list and
native total calculation. Existing real CQL mutation/MLS methods provide
LocalQuorum/retry5 and LocalSerial/no-retry LWT semantics. All queries use
bound parameters; credential/prekey request formatting is redacted.

Finite controls cover authentication/capacity/key precedence, same-ID
upsert exemption, different-type admission, temporary replacement, totals,
all-key validation before writes, millisecond timestamp/no last-active, and
propagation of duplicate-MLS failure without invented compensation.

This is an internal registration storage/core boundary, not a registered
public POST handler. No new native HTTP comparison, real storage execution
or original Client test execution is claimed. Account/login-code admission,
legalhold augmentation/public prohibition, custom internal reauth policy,
consumable queue, actual old-client deletion queue/Dynamo lock cleanup,
Galley, synchronous event and new-client mail composition remain required.
Deletion API/consumer and last-active callbacks also remain pending.
Whole Brig/535/eight-Go/operations/all-entity switchback/image/selection
review and availability are still incomplete.

Same-author slice review under the user's explicit review/self-approval
instruction accepts these internal changes within this stated scope; it is
not independent review or whole-service approval. Preserve native
nontransactional prekey/client/MLS writes and reauthentication exemptions;
do not repair Haskell or harden policy during migration.
