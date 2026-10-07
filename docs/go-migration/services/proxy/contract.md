# Proxy compatibility contract

Status: **requirements self-approved; executable baseline fixtures outstanding**.
Task: `proxy-contract`. Approval scope and evidence limits are recorded below.
Reference source: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Source map: [inventory](inventory.md). No intentional behaviour exception is
proposed. Unknown runtime details must be characterized against the pinned
reference; they are not permission to omit cases or substitute Go defaults.

## Required observable scenarios

| Scenario family | Required comparisons |
| --- | --- |
| Seven public handlers plus health | Existing paths, suffix/endpath routing, query requirements, status, body and semantic headers |
| Giphy/YouTube/Google forwarding | Exact upstream method/path; duplicate/query escaping/order; injected key position; case-insensitive removal of access_token; GET enforcement and error payload |
| Spotify | Body and configured Authorization; Accept/Content-Type allowance; client Authorization/other headers excluded; source's inbound method acceptance; upstream response status/content type/body |
| Soundcloud resolve | Required URL handling, encoded URL/client_id query, status/content type/body and transport failures |
| Soundcloud stream | Exact configured host and HTTPS enforcement, redirects disabled for the initial request, required upstream 302/Location, empty redirect body and generic errors |
| Failure/retry | 502 retry counts, selected transport retries/backoff, five-second response timeout, cancellation, upstream 4xx/5xx, malformed/empty responses and redirect loops |
| Shared middleware | V0..V19/disabled/development versions; versioned internal rejection; forged version header treatment; request ID; request gzip; parser/method errors and unknown routes |
| Operations | Existing YAML/CLI/secrets mounts, 60-second secret reload, HTTP proxy vars, connection reuse, probe behaviour, metric/log schemas and graceful termination |

The four `proxyServant` handlers compare the incoming method case-insensitively
with GET. Spotify and both Soundcloud handlers use `RawM` without an inbound
method restriction; their documented GET summaries do not enforce GET. Include
POST and a mixed-case method in the baseline fixtures. The reverse-proxy query
filter removes only `access_token`: caller-supplied `key`/`api_key` pairs remain
after the prepended configured credential, including duplicates. These details
come from `Proxy.API.Public:{proxyServant,proxy,safeQuery}` and
`Wire.API.Routes.Public.Proxy:ProxyAPI`; do not substitute framework defaults.

Source obligations are `Proxy.API.Public` (provider logic), `Proxy.Env` (client
limits/reload), `Proxy.Run` (middleware/shutdown), `Wire.API.Routes.Public.Proxy`
(routing), and `Wire.API.Routes.Version.Wai` (versions). Preserve health empty
200 and the existing public/internal authentication boundary. Both query and
outgoing header effects are part of parity, not just returned response bodies.

Tests include wrong provider methods, missing URL, extra path segments, repeated
and case-varied query keys, escaped bytes, hostile Soundcloud host/scheme URLs,
client-supplied provider credentials, missing secrets and malformed/gzip input.
Use only synthetic credentials, fixed request IDs and fake provider endpoints.
Production TLS verification stays enabled. A test-only HTTP allowance already
present in the reference does not justify an insecure candidate TLS transport.

## Comparisons and nondeterminism

Status, byte bodies and all application headers are compared; JSON object member
order alone can be compared semantically while preserving missing/null distinctions,
numbers and array order. Transport-generated Date/Server/Connection/Transfer-Encoding
are recorded separately and may vary between HTTP servers; application values such
as Content-Type, Content-Encoding, Location and request IDs must not be dropped.
Content-Length is checked against observed bytes where supplied, even when one
implementation legitimately uses chunked transfer. Gzip comparisons validate and
decompress both bodies and still compare encoding headers and payload exactly.

Provider observations (method, escaped path, query pairs, relevant headers, body,
attempt count) are compared directly. No arbitrary field deletion, body masking,
list sorting, error suppression or broad URL normalization is allowed. Normalizer
changes require their own contract review and negative comparator tests.

## Evidence and selection

The independent runner must emit pinned upstream/reference/candidate identities,
reference image/source provenance, fixture/config/comparator identities, every
required scenario result, side-effect observations and sanitized mismatch details.
Skipped/blocked/missing cases cannot produce success. Mock-versus-mock self-tests
are runner tests only. Real Haskell-versus-Go comparison requires an authorized
isolated environment; no existing full integration suite is authorized here.

Operational thresholds must be recorded before benchmarking. Default chart
rendering continues selecting Haskell; explicit selection changes only the proxy
image, retaining service name/config/probes and enabling a tested switchback.
Availability requires the full scenario matrix, default-render tests and review
of the actual candidate evidence. Requirements approval does not satisfy a
lifecycle gate while baseline fixtures and runtime verification are missing.

## User-authorized self-review, 2026-10-07

Reviewer: Codex, the same actor that authored these documents. The user explicitly
requested: "Please review these yourself, to they meet our goals and technicall
assure that what we refactor offers the same contract? if so self approve."
This is delegated self-review, not independent or human technical review.

**Decision: approve the optional-service design and these source-derived proxy
requirements for continued preparation.** Keeping whole services separate,
Haskell as default/reference, fork work on `go-migration`, and explicit per-service
image selection with switchback meets the stated migration goals. No behaviour
exception, deployment switch or removal of Haskell is approved.

Reviewed the pinned source's public route types and provider handlers, environment,
middleware and version handling. Added explicit method/query edge cases above.
The isolated synthetic Docker environment described in the execution plan is
suitable for characterization; existing integration suites and production remain
outside this review. No runtime was executed as part of this approval.

Reviewed `tools/parity/compare.py` as an offline foundation: exact status/headers,
typed JSON/byte comparisons, ordered effects, mandatory scenario/assertion
coverage and explicit `parity_verified: false` prevent its own result being a
verified-runtime claim. It still lacks live capture, transport/gzip rules and
the cargohold mappings required by these contracts. Self-tests are not parity.

Outstanding before full contract/task completion: executable source-derived
fixtures with case IDs and reference captures; characterization of parser,
redirect, timeout, log/metric and shutdown behaviour; numeric operational
thresholds before load testing. Published image configuration matching evaluated
Nix output identities is useful provenance evidence, not an independent binary
rebuild. Default Haskell build/unit/chart checks have not been executed. There is
currently no Go service implementation to approve as contract-equivalent.
