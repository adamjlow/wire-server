# Federator security findings

Mirror the current service boundary; no production/security exception approved.

- External HTTP listener trusts the ingress-provided URL-encoded certificate and
  only checks its DNS identity against origin SRV records. It deliberately does
  not revalidate the certificate chain/expiry: current mTLS ingress does that.
  Direct untrusted access or ingress failing to replace attacker-supplied headers
  can forge this trust boundary. Keep external listener private to trusted ingress;
  future end-to-end authenticated ingress metadata needs explicit contract review.
- Federation discovery accepts the first SRV wire-order target, including private
  destinations, and checks allowNone/allowDynamic per use. DNS-selected egress is
  an inherited SSRF/rebinding boundary when allowAll is enabled. Documented future
  egress policy/explicit destination controls must preserve legitimate federation.
- Legacy CN-only client identity fallback and wildcard matching mirror source;
  verified outbound TLS uses standard Go chain/hostname verification. Certificate
  validation never disables TLS or uses InsecureSkipVerify. Invalid reload retains
  the last good context; CA root change is actually tested to reject untrusted TLS.
- Haskell DEBUG logs entire outward request bodies. Avoid retaining sensitive
  captures and review operational logging before enabling debug. Go does not copy
  that body logging; startup/transport diagnostics are generic. HTTP/status/effects
  remain tested against the reference; diagnostic words/Go runtime gauges differ.
- Domain-labeled metrics can grow with distinct domains, as in the source. Avoid
  exposing raw private listeners to untrusted traffic. Request headers are bounded,
  bodies stream with cancellation; internal policy/error decoding is bounded and
  malformed required brig policy fails500. Incoming validation repeats live policy
  before certificate discovery, matching source; no stale cached permission.
- The required scanner on2026-10-09 found newly published Go1.26.8/x/net0.59.0
  HTTP2/TLS/parser issues (including GO-2026-6617). Candidate and optional compiler
  pins were patched to1.26.9/x/net0.60.0, official archive SHA256 verified, final
  module scan reports no vulnerabilities. Primary advisory:
  https://pkg.go.dev/vuln/GO-2026-6617 . No scan bypass or security exception.
- At initial federator qualification the four earlier peer artifacts used Go1.26.8;
  that historical run established functional behavior without renewing security.
  The subsequent user-requested [patched rebuild](../../../docs/go-migration/qualifications/go-patched-20261009/README.md)
  rebuilds/requalifies all four on Go1.26.9, passes their module scans and original77
  with this unchanged patched federator. Use the new identified artifacts; old
  Go1.26.8 binaries remain historical, not patched. Haskell remains default.

Synthetic fixture certs/provider credentials are existing test artifacts, never
production identities. Private runner uses owned internal networks/no host ports,
nonroot/read-only mounts and cleanup. No remote push or production rollout.
