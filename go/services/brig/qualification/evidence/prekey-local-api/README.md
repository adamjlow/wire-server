# Local prekey API and actual native/Go claim comparison

Source `0f1f978787c40106f8a4ac9fafbdfec134f32782`, service SHA256
`6c7de6233a2475de89f16109180fc15003fe8cb2d835c885012aab21ac61c83e`.
Full pinned Go1.26.9 checks/static normal units pass; tagged stores compile only.
Static runner/SDK identities and full logs retained. No new original compiled
suite or native Haskell build; earlier four original Client cases retain their
exact source scope. Whole Brig remains incomplete/unavailable.

Local qualified single/bundle GET/HEAD and legacy unqualified UntilV2 routes now
bind actual claim store, real Galley guard and deletion service. Single claims
perform Galley guard -> claim -> missing-key client lookup -> existing-client
execDelete with connectionNothing. Bundle reads clients -> guards full client
set -> sequential claims, omits missing keys without noPrekeys cleanup. Remote
routes do not access local storage; outgoing federation remains unfinished.

Guard PUT /i/guard-legalhold-policy-conflicts uses ProtectedUser tagged object,
glhUserClients map with numeric sorted/deduplicated IDs, Request-Id and noZUser.
Native200/404 allow;403 old-client label preserves exact old-client error;
other403 defaults to missing-consent. Actual HTTP units exercise200/404/both403/
unknown403 and four-attempt500 retry failure. Existing RPC helper now retains
allowed403 body (registration caller ignores body, so its behavior is unchanged).
Focused units also prove denial before storage, missing-client no-op, existing
missing-key cleanup/order/nilconnection, cleanup/claim failures, bundle omission
without cleanup, route versions/ID normalization and exclusion of remote local
claims. Optimistic exhaustion binds actual generic warning; counters/other
missing-key warnings and full metrics surface remain pending.

Frozen fixture randomPrekeys=true is unchanged. Native first claims ordinary7,
then65535 twice, returns a bundle of the last-resort key and404 for an unknown
client. Native restages the original ordinary key through its real upsert API;
Go repeats the exact five status/media/JSON comparisons on the same user/client
and leaves only65535 in Cassandra. Actual native Galley guard and Go global
local lock/selection/removal are exercised. Both independent creations' times
remain unnormalized. Seven lifecycle admission controls and original17 paired
controls pass; native restart reads Go-created client/MLS/prekeys and deleted
native-client state, consumes Go-created ordinary key7 and confirms retainedMLS
on recreation. All13 managed exits0/runner0/cleanup0; independent audit empty.

This is random-mode direct API/state proof with Go Brig/Gundeck/native remainder,
not final eight-Go/whole535/all-entity three-mode qualification. Optimistic Dynamo
strategy has signed HTTP units only; no live native/Go Dynamo claim comparison.
Existing-client-with-no-keys corruption repair is unit/composition proof, not a
new live corruption witness. Real legalhold403/feature matrix, parser literals,
remote federation/bundle/multi-user, concurrent real claims, metrics/SDK exception/
Read Word32 corners and full service qualification remain pending.

To make room before build, two completed owned SDK output directories relocated
to disk:36 files/380041881 logical bytes; every SHA256/UID/GID/mode verified and
logical directory paths preserved by links. Initial copy lacking FOWNER failed
before any original mutation, then constrained copy succeeded and only verified
duplicate originals removed. No compiler cache/native dependency/evidence/binary
pruned; relocation receipt retained. Current Docker namespace cleanup verified.

Same-author scoped review under explicit self-approval delegation accepts stated
implementation/tests/state proof only; not independent/whole-service approval.
Haskell/reference/oracle/fixture/assertion/default/security policy unchanged.
Native best-effort Dynamo locking and nontransactional cleanup remain documented
inherited concerns; no unrelated native repair performed.
