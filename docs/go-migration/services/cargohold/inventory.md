# Cargohold inventory

Inspected reference: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Task: `cargohold-inventory`; author: Codex. Source observations only; no runtime
parity or candidate readiness is claimed. Reference source was not modified.

## Service boundary

`services/cargohold/exec/Main.hs` and `CargoHold.Run` load existing YAML options
and serve federation, public and internal APIs. The default config is under
`/etc/wire/cargohold/conf/`. Public routes are in
`libs/wire-api/src/Wire/API/Routes/Public/Cargohold.hs`; implementations are
`CargoHold.API.Public`, `API.V3`, `API.Legacy` and `API.Federation`.

| Interface | Observed scope |
| --- | --- |
| User asset V3/V4 and legacy paths | Available before public API V2, including `/assets/v3`, `/assets/v4/<domain>/<key>`, conversation/plain/OTR legacy downloads and token mutations |
| User asset V2+ API | Versioned `/assets` uploads; qualified downloads/deletes; unqualified token mutations |
| Bot/provider APIs | `/bot/assets` and `/provider/assets`, principal-specific ownership and Location paths |
| Internal | Empty GET `/i/status`; GET `/i/assets/<key>` returns a JSON signed URL without public ownership checks |
| Federation | POST `/federation/get-asset` returns availability; POST `/federation/stream-asset` streams authorized bytes using origin-domain context |

`Z-User`, `Z-Bot`, `Z-Provider`, `Asset-Token`, `asset_token`, `Z-Host`, federation
origin and optional remote-IP headers are protocol inputs. Public bearer-token
verification happens at ingress; services use the existing internal-auth header
contract. `Asset-Token` takes precedence over the query token. Shared Servant
parsing/error/Accept/HEAD behaviour needs runtime characterization.

## Dependencies and persisted effects

Primary storage is S3-compatible object storage via Amazonka, not Cassandra or
Postgres. `CargoHold.App.Env` owns AWS/HTTP/HTTP2 clients and options. User uploads
ask Brig for account information; active team users use `maxTotalBytes`, active
non-team and ephemeral users use `maxTotalBytesStrict`; bot/provider uploads use
the larger limit. Unverified and absent users have distinct errors.

`CargoHold.Federation` calls the existing federator over its internal HTTP/2
protocol for remote availability then streaming. Local assets redirect; remote
assets stream through the federator. Remote deletion is unsupported. A complete
candidate needs Brig/federator peers; replacing either is outside this scope.

`libs/wire-api/src/Wire/API/Asset.hs` defines asset keys
`3-<retention-number>-<uuid>` and retention 1..5: eternal, persistent, volatile,
eternal-infrequent_access, expiring. Default upload retention is persistent.
Volatile expiry is 28 days; expiring expiry is 365 days. Responses include key,
local domain, optional token and optional millisecond-precision expiry.

`CargoHold.S3.mkKey` stores V3 objects as `v3/<retention-name>/<uuid>`. Metadata is
`user`/`bot`/`provider`, optional `token`, optional URL-escaped JSON `wire-metadata`.
Reads accept both raw JSON and encoded audit metadata; owner lookup prioritizes
user, then bot, then provider. Tokens are random 16-byte URL-safe base64 values.
Uploads force stored content type to `application/octet-stream`. Token updates
copy the object with replaced metadata, preserving audit metadata and bytes.
Legacy plain objects use bare asset UUID; OTR uses `otr/<conversation>/<asset>`.
Plain legacy reads require `zasset` metadata containing `public=true`; OTR reads
require parseable user metadata. These keys/metadata must remain interoperable.

## Upload, ownership and security

Uploads are streaming multipart/mixed with metadata first and bytes second.
Each part declares Content-Type/Content-Length; the bytes part may also declare
Content-MD5. The source parser allows only declared header names, rejects
duplicates, and validates metadata JSON/MIME types, declared length and configured
limits. It consumes exactly the declared payload length, excluding boundary
trailers; truncated data returns `incomplete-body`. S3 uses multipart upload with
abort/error handling; characterize residual objects on incomplete/failing upload.

Downloads accept owner or matching token; a missing stored token makes an asset
public when no token is supplied. Wrong credentials yield not-found responses
to avoid disclosing private assets. Delete/renew/delete-token require ownership;
deleting the token makes the asset public. Federation passes a qualified
requester to `API.V3.checkMetadata`, but that function discards the domain before
comparing against the stored unqualified principal. A remote same-ID principal
therefore takes the source's owner branch irrespective of domain. Legacy OTR
downloads require parseable stored user metadata but do not compare its value to
the requester. Both are security-sensitive source findings requiring explicit
characterization and contract resolution; domain-aware ownership must not be
advertised as existing behaviour. See the [self-review](contract.md).
`API.Error` maps the static Wire errors defined in
`libs/wire-api/src/Wire/API/Error/Cargohold.hs`.

Keep TLS validation and established ingress trust boundaries. Validate UUIDs,
domains, tokens, MIME input and lengths before side effects. Use synthetic data;
protect AWS credentials and signed URL signatures from diagnostic output.
Audit logging intentionally includes metadata according to existing config;
validate its field semantics without committing real personal data.

## Configuration and operations

`CargoHold.Options` declares bind endpoint, Brig/federator, S3 endpoint/bucket,
addressing style (default path), download endpoint, optional CloudFront, Scality
compatibility, multi-ingress map, upload limits, URL TTL, audit enablement, local
domain, disabled versions and log options. AWS credentials use the standard
provider discovery chain; current charts provide AWS environment variables.

`CargoHold.S3.signedURL` chooses the configured download endpoint; configured
multi-ingress requires a matching `Z-Host`, otherwise not-found. CloudFront takes
precedence and signs a canned policy using RSA-SHA1 with a 300-second lifetime
and CloudFront-specific base64 mapping. Preserve this protocol primitive; do not
introduce new cryptographic choices as part of language migration.

`charts/wire-server/templates/cargohold/` is the actual packaging location (there
is no standalone `charts/cargohold/`). Existing config/secrets mounts, service
identity, service account, environment variables and probes must stay valid.
`CargoHold.Run` adds version, request ID, Prometheus, gzip and error middleware;
AWS auth-expiry metrics are collected separately. Capture exact metric names,
log schemas, version handling, shutdown/cancellation and streaming memory limits.

## Existing evidence and unresolved checks

- `integration/test/Test/Cargohold/` covers upload/download, roundtrip/tokens,
  metadata compatibility, remote/federation streams, multi-ingress, limits,
  metrics and asset audit logging. Source inspected only; no integration run.
- Current `cargohold.cabal` declares a unit suite with S3/audit-log tests and an
  old integration executable. Both remain Haskell reference material.
- `CargoHold.API.V3` and `S3` supply concrete multipart/key/metadata behaviour;
  generated Go IDs/tokens and signed URLs require constrained comparison rather
  than deleting fields or ignoring entire URLs. Cross-runtime reads of the same
  seeded S3 objects provide stronger switchback evidence than isolated roundtrips.
- Missing characterization: exact parser errors/HTTP headers; incomplete upload
  residual effects; SDK signing/addressing/credential-refresh parity; federation
  HTTP/2 framing/error mapping; metric/log fields and shutdown/resource thresholds.
- Native Haskell tooling is missing locally. Docker can host isolated references.
  [Reference discovery](../../plans/reference-provenance.md) records immutable
  published images whose configs match source-evaluated Nix output identities;
  this is not an independent binary rebuild or executed unit/parity evidence.

Next tasks: review complete contract; establish fake Brig/federator and trusted
local TLS/S3 fixtures; pin maintained SDK/YAML/toolchain; implement all interfaces;
run full differential plus shared-store switchback; verify default chart rendering
and explicit image selection. Unknowns or partial endpoint coverage prevent
`verified`/`available` milestones.
