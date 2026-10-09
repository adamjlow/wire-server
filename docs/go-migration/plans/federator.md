# Optional Go federator

User's 2026-10-09 request to proceed with the next service extends the existing
whole-service migration and same-author review delegation to federator. Reference
f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; pinned Go1.26.9 (security patch after failed scan). Haskell stays default.
Only owned internal Docker networks, synthetic data, trusted fixture TLS, no host
ports and guaranteed cleanup are authorized for original test execution. This is
the same private environment approved for previous service migrations, extended
to federator cases. No remote push, production switch or security exception.

1. Pin source inventory and discover original Federator, Errors and federation
   cases using the existing immutable runner with network disabled.
2. Freeze finite reference contract in a separate contract task: health, routing,
   incoming certificate/name and allow-list checks, outgoing DNS/mTLS HTTP2,
   internal header filtering, response/error/streaming, refreshed policy and
   certificate configuration, metrics, shutdown. Run original relevant assertions
   against Haskell; add direct comparisons only for concrete uncovered boundaries.
3. Claim isolated Go module; implement health and forwarding immediately, compare
   with the actual Haskell process, then finish whole-service behavior. No shared
   Go framework or Haskell component split.
4. Execute frozen matrix and original cases with intended qualified Go peers;
   one bounded operations smoke and Haskell-to-Go-to-Haskell process switchback.
   Verify optional nonroot image, existing Helm lifecycle hook and default render.
5. Delegated same-author review and adjacent machine-state gate publication.
   Stop once finite acceptance passes; retain failures and unrun native checks.

Security: retain outbound TLS chain/hostname verification and mTLS client identity;
validate incoming certificate identity using source trust boundary (trusted ingress
verifies chain), domain/path/method/headers and live federation policy. Never trust
arbitrary user URLs, execute input, leak private keys or disable TLS validation.
Document serious inherited ingress/header/logging/DNS concerns separately. Use
maintained dependencies with pinned versions, license and vulnerability receipts.
Go security negatives cover malformed routes/certificates, mismatched hostname,
untrusted remote TLS and denied policy. No Haskell/fixture/default modifications.

Commands: make migration-check migration-test; scoped tools/migration/check.py
--base FULL_SHA --task TASK; pinned compiler with python3 go/check.py federator.
Build commands end with the required output filter and inspect actual build exit.
Reuse tools/parity/docker environment/control and exact existing peer receipts.
Evidence stays in service inventory/oracle and module review. Native Haskell tools
are unavailable locally; retain existing upstream CI and report checks unrun.

Completed execution (2026-10-09): source845956f630b791645a9c12d23918d8c9c3bc7507,
binaryfbe1d6d62762e79da64a15a6e06af9ce7448ab2ace0ca33530ec261aa46a4595.
Final original77 with all five Go services/remaining Haskell peers and frozen35
pass without failures/skips, process0/cleanup0. Pinned Go1.26.9/x/net0.60.0 quality,
vulnerability/license/race checks pass. Actual identical-binary nonroot image and
default-preserving/fail-closed Helm selector pass; Haskell->Go->Haskell unchanged
configuration and loaded stream drain pass. Delegated review/evidence retained
in go/services/federator/REVIEW.md; intermediate failures/aborts in evidence/HISTORY.md.
Earlier four Go artifacts need security-patched rebuild/requalification before
production; mixed-run functional evidence does not renew their security status.
Finite acceptance complete; native Haskell checks unavailable/unrun, defaults
and shared assertions untouched, no remote push or production switch.
