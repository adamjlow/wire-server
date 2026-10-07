# Cargohold compatibility contract

Status: **draft for separate human review**. Task: `cargohold-contract`.
Reference source: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Source map: [inventory](inventory.md). No intentional behaviour or storage change
is proposed. Every supported interface belongs to the whole-service boundary.

## Required scenario matrix

| Family | Required comparisons and persisted effects |
| --- | --- |
| User V0/V1 and V2+ routes | Upload/download/delete/token paths, qualified keys, relative Location, version gating, status/body/error/headers |
| Bot/provider routes | Principal headers/ownership, upload limits, download/delete and respective Location paths |
| Upload/account checks | Brig active team/non-team/ephemeral/unknown/unverified account responses, exact limit selection, boundary lengths and negative errors |
| Multipart | Required metadata-first order, allowed/duplicate headers, MIME/JSON parsing, declared lengths, truncated streams, optional MD5, arbitrary chunk boundaries, gzip and disconnects |
| Object representation | `v3/<retention-name>/<uuid>`, canonical asset keys, octet-stream content type, owner/token/audit metadata and stored bytes |
| Authorization/tokens | Owner/token/public reads; wrong-token non-disclosure; header-over-query precedence; owner-only changes; renew invalidates old token; delete-token makes public |
| Retention | All five names/numbers; persistent default; volatile 28-day and expiring 365-day millisecond expiry; configured store lifecycle assumptions |
| Signed downloads | S3 path/virtual/auto addressing, endpoint override, configured TTL, multi-ingress match/failure; CloudFront precedence, RSA-SHA1 canned policy/300-second TTL and base64 rules |
| Legacy assets | Plain/conv/plain/OTR routes; existing UUID and `otr/<conversation>/<asset>` keys; legacy metadata access rules and signed URL output |
| Federation/internal | Unversioned health/internal signed URL; origin-qualified availability/stream authorization; outgoing HTTP/2 remote availability/stream; remote errors/deletion rejection |
| Audit/operations | Enabled metadata requirement, encoded/raw audit metadata reads and preservation, log field schemas, metrics, existing config/CLI/AWS discovery/probes, streaming memory/cancellation/shutdown |
| Failure and switchback | S3 retries/error/abort/incomplete-upload effects; Haskell-written objects read/modified by Go and Go-written objects read/modified by Haskell, with token/audit/bytes retained |

Use `CargoHold.API.{Public,V3,Legacy,Federation}`, `CargoHold.{S3,AWS,Federation,
CloudFront,Options,Run}`, `Wire.API.{Asset,Error.Cargohold}`, public/internal route
definitions and `libs/wire-api-federation/.../API/Cargohold.hs` as source anchors.
Existing integration scenarios under `integration/test/Test/Cargohold/` are
reference inputs, not evidence of a successful migration.

The source's known static errors must remain: asset not-found 404/not-found;
ownership 403/unauthorised; size 413/client-error; invalid length 400/invalid-length;
unverified user 403/unverified-user; missing user 403/not-found; incomplete bytes
400/incomplete-body; missing audit metadata 400/missing-audit-metadata. Compare
actual messages/serialization/content type against the reference. Shared parser,
unknown-route and federation errors require characterization rather than guessing.

## Security and persisted compatibility

Existing ingress headers define authentication; preserve the trust boundary and
strictly parse IDs/domains/tokens. Federation's qualified user must not acquire
local ownership by sharing the same UUID. Wrong credentials must not disclose
private asset existence. No new Go-only schema, object prefix or metadata format.
Validate input lengths/headers and resource limits before uploads. Use bounded
streaming and cleanup for cancellation/failure. Keep trusted TLS and never log
real credentials, personal audit data or signed URL signatures in artifacts.

Switchback uses a dedicated test S3 store, never production. Cross-implementation
scenarios use the same object's bytes/metadata and perform reads, token renewal,
public/private transitions, audit preservation and deletion in both directions.
Isolated roundtrips alone do not prove storage interoperability. Also seed legacy
and raw/URL-encoded audit metadata objects through the independent fixture tool.

## Constrained comparison

Compare status, application headers, JSON shape/types/values, array order, bytes,
provider effects and S3 metadata. Preserve missing versus null and owner/token
semantics. Transport Date/Server/framing differences are recorded separately;
Content-Type, Content-Encoding and Location semantics remain required.

Generated keys are captured only after validating version/retention and UUIDv4,
then consistently mapped through subsequent requests, Locations and object paths.
Generated tokens must decode to 16 bytes and survive authorization/cross-runtime
checks; map each token consistently, never remove all token fields. Expiry must
have millisecond precision and the correct retention duration relative to observed
request time within the scenario's predeclared clock tolerance.

Signed URLs must retain target scheme/host/bucket/object, algorithm, credential
scope, expiry and signed headers. Verify signatures and actual reads against the
test store. Only validated signing timestamps/signatures can be normalized for
cross-run clocks; malformed signatures, wrong hosts, incorrect TTLs and lost
parameters must fail. CloudFront signatures must verify with the trusted generated
test key. No broad URL stripping or blanket body/header exclusions.

## Evidence and operational availability

Reports pin upstream/reference/candidate SHA and image, source provenance,
config/fixture/comparator hashes, supported peer/version combinations, every
scenario and sanitized mismatch/storage observations. Unsupported, skipped,
blocked or absent required cases fail the gate. Reference/candidate health checks
and candidate unit tests do not count as like-for-like parity.

Before load tests, record agreed upload/download sizes, concurrency, memory,
latency/error thresholds and abort criteria. Verify default chart output against
the unmodified Haskell baseline; explicit selection changes only cargohold's image
while preserving config/service account/secret mounts/probes. Haskell builds and
ordinary installations remain usable without Go. Production switching requires
a separate operator-approved task. This draft authorizes no rollout.
