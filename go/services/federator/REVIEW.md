# Federator review

Review status: approved for optional availability on the finite supported matrix.
Reviewer: codex, same-author review explicitly delegated by the user and continued
by the 2026-10-09 request to proceed with the next service. This is not an
independent review or authorization to push, deploy or change Haskell defaults.

## Exact tested artifact

Reference upstream/source: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e.
Candidate source: 845956f630b791645a9c12d23918d8c9c3bc7507.
Candidate SHA256: fbe1d6d62762e79da64a15a6e06af9ce7448ab2ace0ca33530ec261aa46a4595.
Local image: sha256:9cf75f4fb355c3b3d4c4be4518d16b8732cb3a5ee4f5d1fef7b3cb61b4c3b3c8.
The image contains the identical qualified binary. Metadata/evidence commits do
not change or rebuild those service inputs. Build receipt: evidence/final/build.json.

## Finite acceptance

- Final original 77 suite result: pass, no failures/errors/skips, process0/cleanup0.
  Retained XML/run receipts and original-environment.json identify all five binaries.
  Proxy5 + cargohold27 + gundeck40 (including cannon35) + federator5 pass.
  Run federator-five-go-20261009-4 selects
  exact identified Go proxy/cargohold/cannon/gundeck/federator processes, remaining
  Haskell peers and unchanged upstream assertions in owned private Docker networks.
- Frozen 35 actual Haskell differential captures all pass, process0/cleanup0:
  evidence/final/comparisons.json and forwarding.json. Cases cover both listeners,
  health, route/method/query errors, required and malformed certificates, identity
  mismatch, three inward components and filtering, outbound DNS/HTTP2/mTLS/errors,
  dynamic allow/deny, actual trust-root rotation and invalid reload retention.
  Only native untrusted-TLS diagnostic text is normalized after asserting its525
  status/label. Stream bytes, other status/errors and selected headers remain exact.
- Real Haskell->Go->Haskell process switchback passes with identical configuration
  hash and responses: evidence/final/switchback.json. No service-owned database
  changes. In-flight Go stream completes200 with preserved bytes during SIGTERM;
  exit0 in0.564s. One operations smoke covers40 requests/concurrency2, ~12MiB RSS,
  p95~0.5ms, bounded against the reference; idle shutdown exits0 within4s.
- Pinned Go1.26.9/x/net0.60.0: format, tidy-diff, module verification, vet, race/unit,
  build, vulnerability and strict license checks pass: evidence/final/quality.log.
  Eight focused regressions cover domain/RPC/IP, SAN/CN/wildcard, DNS wire ordering,
  verified HTTP2/untrusted and wrong-host TLS, real h2c/per-use policy, metric labels
  and streaming, malformed policy, generated UUID/header propagation.
- Actual image health200, nonroot65534, read-only, capabilities dropped, private
  network/no host ports and SIGTERM exit0/0.072s pass. Actual Helm render default
  bytes are unchanged; selecting Go changes exactly one federator image, malformed
  or missing targets fail closed. Source chart has no exec lifecycle hook.
  Receipt: evidence/final/image-smoke.json. Existing federation fixture values
  enable the target only for proof; upstream federation defaults remain unchanged.

## Review judgment and limits

Review follows the frozen source inventory/contract, then checks concrete source
boundaries missed by existing assertions: DNS wire order, required policy shape
and repeated authorization, streaming/filtering, certificate trust and origin
request-ID generation. Concrete mismatches were corrected before qualification.
No additional speculative validation is required once this finite matrix passes.
Whole-service isolation and opt-in chart postrender preserve upstream merging and
Haskell support. Original Haskell assertions and reference captures were not edited
to make the candidate pass. See evidence/HISTORY.md for failures, aborted candidate
runs and corrections; early partial passes do not qualify the final source.

Native Haskell builds/type checks/format/unit checks were not run: GHC/Cabal/Nix
are unavailable locally and Haskell packages are unchanged. Existing upstream CI
remains. Legacy federator-integration/federator-test executables are absent from
pinned runner PATH, not claimed as run. The retained compiled original suites and
actual Haskell differential reference were executed in the established harness.
This qualifies the finite supported contract, not every incidental runtime detail:
Go runtime gauges and generic diagnostics differ; forwarded bodies are not logged.

Serious concerns and inherited boundaries are in SECURITY-NOTES.md. Incoming
certificate trust still requires the existing verified private mTLS ingress;
DNS-selected egress, legacy CN fallback and domain metric cardinality remain.
Outbound TLS chain/hostname verification is enabled throughout. The scanner found
new Go1.26.8/x/net0.59.0 issues; compiler/dependency were patched without bypass.
Final federator scan is clear. Earlier four peer artifacts retain historical
functional evidence but require patched rebuild/requalification before production;
the mixed run does not renew their security qualification. No production switch,
remote push, secret generation or security exception is part of this review.
