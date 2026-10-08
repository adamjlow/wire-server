# Cargohold compatibility contract

Status: **approved for implementation under the user's 2026-10-08 exact-parity
disposition and delegated self-review; runtime verification still required**. Task: `cargohold-contract`.
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
| Federation/internal | Unversioned health/internal signed URL; reference UUID-based availability/stream authorization; outgoing HTTP/2 remote availability/stream; remote errors/deletion rejection |
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
parse IDs/domains/tokens according to the pinned reference's accepted syntax.
Preserve the pinned source's unqualified owner-UUID federation comparison under
the explicit user disposition below. Different-UUID requests still require the
correct token or a public object; the matching-UUID bypass is a documented serious
security concern. Do not claim domain-aware authorization in this migration. No new Go-only schema, object prefix or metadata format.
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
a separate operator-approved task. This contract authorizes no rollout.

## User-authorized self-review, 2026-10-07

Reviewer: Codex, the same author; self-review explicitly delegated by the user
in the request quoted in [proxy's review](../proxy/contract.md). No independent
reviewer or human technical review is claimed.

**Decision: approve the whole-service scope and persisted-format/switchback
requirements; withhold full cargohold contract approval.** The previous draft
incorrectly described a desired security property as existing behaviour:

| Finding | Pinned source evidence | Required disposition |
| --- | --- | --- |
| Federation loses the requester's domain in the owner check | `CargoHold.API.Federation:checkAsset` passes a qualified principal, but `CargoHold.API.V3:checkMetadata` takes `qUnqualified qown` and compares it directly to the stored unqualified principal | Independently characterize a foreign-domain requester with the owner's UUID, missing/wrong token, on both availability and streaming routes. A domain-aware check is an intentional behaviour change, not like-for-like parity. Resolve in explicit contract/security work before implementing this boundary or claiming complete equivalence. |
| Legacy OTR does not compare stored user with requester | `CargoHold.S3:getOtrMetadata` requires a parseable stored user; `CargoHold.API.Legacy:downloadOtr` ignores its value (`found _`) | Characterize same/different requester and absent/malformed stored owner. Do not claim that this endpoint already enforces ownership, silently tighten it, or introduce a candidate bypass to obtain a green parity report. |

These are source findings, not a claim of demonstrated exploitation through a
deployed ingress/federator. No security exception or changed authentication
contract is approved by this review. Safe preparation and unrelated service
work can continue; full cargohold availability remains contingent on resolution.

Also make these source quirks explicit in executable fixtures before choosing a
Go multipart implementation: `API.V3:boundary` consumes an arbitrary line beginning
with `--`, without comparing delimiter values; the payload's Content-MD5 header
is accepted but unused; `contentLength` uses `parseOnly decimal` without an
end-of-input check. `S3:uploadV3` isolates the declared byte count, ignoring the
remaining trailer, and checks incomplete length after the streaming upload result.
Do not assume standard-library multipart parsing or automatic MD5 validation
has the same accepted inputs, errors or persisted effects. Characterize residual
objects and cleanup before approving any stricter validation as equivalent.

The matrix covers the correct complete service boundary, including bot/provider,
versioned and legacy APIs, Brig, federation HTTP/2, S3/CloudFront, audit metadata,
and both switchback directions. Missing case IDs/reference captures, validated
generated-value/signature comparison, predeclared clock tolerances, numeric load
thresholds and real runtime/selection evidence still prevent task completion.
Neither mock comparison nor source review establishes an equivalent Go service.

## Resolved disposition and implementation approval, 2026-10-08

User decision: "we must mirror how the backend works today but note down any
serious security concerns/improvements." This explicitly resolves the previously
escalated choice in favor of existing behavior during migration. Record security
improvements separately; implement neither a Go-only authorization change nor an
unrequested Haskell security patch. Approval applies to behavioral compatibility,
not a claim that the inherited behavior is secure or production cutover approved.

The real reference observations in [oracle/reference.json](oracle/reference.json)
and [capture identity](oracle/run.json) resolve the preceding characterization
requests. These are copies of the separately completed reference task's exact
artifacts, not new observations or candidate results. Ten federation controls
show matching owner UUID accepts absent/wrong tokens from a foreign origin on
both availability and streaming; different UUID rejects absent/wrong tokens and
accepts the correct token. Four legacy OTR controls show either requester reads
when stored uploader metadata is parseable; absent/malformed uploader returns
404. Six multipart controls retain unequal delimiters, ignored Content-MD5,
length suffixes, absent closing trailer and incomplete-body partial-object effects.
Use these exact expectations; no blanket normalization of authorization/errors.

The serious federation concern and improvement are recorded in
[security concerns](oracle/security-concerns.md). Legacy OTR behavior is recorded
as existing shared encrypted-asset access rather than asserted to be an exploit.
Deployment reachability/impact is unverified; findings describe tested service
behavior, not demonstrated exploitation of a deployed system.

The implementation acceptance matrix and numeric operational thresholds are in
[the completed plan](../../plans/cargohold-implementation.md). All 27 original
compiled Haskell functions have passing retained baseline reports under
tools/parity/docker/baselines/haskell-cargohold.xml. Additional source-defined
cases in that finite matrix are mandatory verification work, not preexisting
passes. Generated expiry comparison permits 2 seconds around observed request
time while requiring the correct retention duration and millisecond representation.
Signed URL TTL comparisons permit 2 seconds between clocks and preserve configured
TTL, target, signing scope and independently verified signatures. Invalid
signatures or changed authorization cannot be normalized. Supplemental assertions
must be reviewed in protected oracle work before candidate parity consumption.

Reviewer: codex (self-review delegated by user), same author, citing the explicit
prior self-review delegation and today's human disposition. The whole-service
contract now accurately describes the reference, retains negative controls and
requires future real candidate/storage/operation/selection evidence. Approve the
contract for implementation; no Go candidate equivalence or availability claimed.
Historical withheld review above remains the record of the earlier finding;
this disposition supersedes its unresolved status.
