# Scoped MLS client reads and version correction

Source `e193a3a7fdf306f1558d9488529a416fe910ae1a`, static service SHA256
`5d9edc95c7c66d6d0048582408d82cb6c28314481d8a62cf5352a9714db3c3c1`,
normal-unit SHA256 `eeef4dbe9895e2753bacea51fa99740da4da067a06e9e7a14ecae18a6d77c960`.
Full pinned Go format/dependency/vet/unit/race/security/license/build/static
normal checks pass; tagged stores compile only. No new dependency or host install.
SDK output uses fresh owned disk directory; stale owned build-cache entries
removed only with no running jobs. Retained binaries/evidence unchanged.

Internal MLS clients list/single and federation V0/V1 list/V3 single implemented.
Supported suites1/2/7/5 select native signature schemes. Count reads actual
ref/data rows, completely decodes TLS presentation syntax, retains malformed
rows, deletes decoded unusable lifetimes/sources before full client lookup,
including native empty-reference deletion. Native counting does not perform
upload-time signature/credential verification. Exact picosecond maximum lifetime
and native nonminimal VarInt/Word16 hexadecimal behavior preserved. Unknown
clients return requested ID/mlsFalse; absent selected signature key is omitted.
Focused units cover strict truncation/trailing data, lifetime bounds, syntax-only
acceptance, malformed retention/expired deletion/order/failure and V0/V1/V3 routing.

Versioned RPC work found and corrected two real candidate bugs: outbound native
version header is X-Wire-API-Version, not Wire-Federation-Version; native preserves
that header on unprefixed requests. Earlier unversioned prekey/client qualification
did not prove versioned behavior. Updated actual h2c tests check the correct
header and incoming versioned routes prove preservation, without changing oracle.

Ten exact native/Go status/media/parsed-JSON controls pass on real initialized
stores: internal list/single/missing/alternate suite/unknown suite, federation
V0 with legacy non-Ed25519 argument (native still selects suite1), V1 list, V3
single/missing/unknown suite. Native-created client signature AQI= is visible only
for the correct suite; no package rows were staged, so live MLS flag isFalse.
No live positive/expired/malformed CQL package-row witness claimed yet. Prior14
public clients/13 federation/5 single/5 multi/7 lifecycle/unchanged17 paired
controls and actual client/prekey/MLS native-Go-native creation/deletion/key/
retention switchback pass. All25 managed exits0/runner0/cleanup0/independent exact
namespace audit empty; no force or new original compiled-suite execution.

Next actual positive/expired/malformed package state/count cleanup proof, then
public MLS key-package upload/claim/count/delete and their original compiled tests.
Native syntax-only counting/nontransactional cleanup and V0 forced suite1 remain
inherited behavior, not repaired. Native malformed-input details/version extremes/
parser aliases and previous prekey timeout/body/transport fidelity gaps remain
unqualified. Other families and full535/eight-Go/all-entity3mode/loaded-operations/
image/default-selector/license/security/full review remain unfinished.
User-delegated same-author scoped review accepts stated evidence, not independent
or whole approval. Haskell/oracle/fixtures/assertions/default/security policy
unchanged; service implementing/compatibility null/review null. Local commits,
no push/deploy authorization inferred.
