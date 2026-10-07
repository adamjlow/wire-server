# Proxy compatibility contract

Status: **draft for separate human review**. Task: `proxy-contract`.
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
Availability requires the full scenario matrix, default-render tests and independent
review. This draft itself does not satisfy a lifecycle gate.
