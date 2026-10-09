# Optional Go federator

User's 2026-10-09 request to proceed with the next service extends the existing
whole-service migration and same-author review delegation to federator. Reference
f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; pinned Go1.26.8. Haskell stays default.
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
