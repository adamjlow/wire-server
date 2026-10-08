# Finite supplemental review

Reviewer: codex (self-review delegated by user), the same actor as the author.
Authority: the user explicitly requested review and self-approval, then instructed
autonomous, appropriately bounded validation and mirroring the current backend
while recording serious security concerns. This is not independent review.

Candidate: `68a36b9137ce26566b2db91902469e8159885656`, static Go 1.26.8 binary
SHA256 `d3591c8f7cbbb5ecbbf9c1425feb29c2b1867e270156337a0cf639eee43f7467`.
All seven final groups used this same binary. [Inventory](final-report.json)
enumerates every actual case and links each observation and identity receipt.
All 112 supplemental records pass; none is missing or skipped. These records
include paired operational observations and switchback scenarios, rather than
112 independent public API endpoints. Original compiled e2e assertions remain
unchanged and are reviewed separately.

| Planned surface | Executed evidence |
| --- | --- |
| Multipart errors, tokens, routing, retention, legacy plain, audit | 38 core records |
| Federation authorization, legacy OTR, streaming and S3 failures | 35 extended records |
| Signed S3, ingress selection, Scality and CloudFront | 11 configuration records |
| Actual Haskell/Go peer calls, audit-inclusive bidirectional persisted-data switchback | 15 mixed records |
| 40 requests per runtime, concurrency 2, 24 MiB signed bytes, RSS/p95 limits, application metrics | 4 operational records plus retained raw samples and before/after metric text |
| Real client disconnect, loaded SIGTERM drain, CLI/config startup | 6 lifecycle records |
| Three concrete malformed metadata gaps found in final source review | 3 schema records |

Failed initial captures are retained. The schema repairs reject malformed data
before S3 writes; unchanged assertions match the reference's exact error strings,
positions and Content-Type. Lifecycle assertions preserve the reference's open
6 MiB part after cancellation under the explicit user disposition. The candidate
drain repair ensures the process waits for the active stream before exiting.
[Security concerns](security-concerns.md) propose separate improvements; the
[original security note](../../../../docs/go-migration/services/cargohold/oracle/security-concerns.md)
retains the federation authorization finding and other inherited behavior.

Review checked primary Haskell service/schema sources, case stimuli, comparison
rules and real response/storage effects. JSON object order is incidental; status,
Content-Type, exact errors, relevant headers, bytes, ownership and token relations
are preserved. UUIDs/tokens are validated before normalization. Signed URLs are
fetched against the owned store, fixed query fields remain, and CloudFront policy
signatures are independently verified. Trace normalization removes only verified
local identity/port differences. Runtime GC/heap and exact latency buckets are
incidental; fixed external RSS/p95 limits and application metric definitions,
labels, thresholds and actual count deltas remain enforced.

The controller uses fixed argv, pinned qualified Haskell runner and Garage images,
synthetic fixtures, non-root read-only containers, bounded resources/timeouts,
internal networks and no published ports. Only owned resources are removed in
finally; failed cleanup propagates failure. Each final controller exited zero,
and every recorded equality and mismatch list was inspected. Process zero alone
is explicitly not a parity assertion. Two focused normalizer tests verify that
bad signatures/clocks/scopes fail and fixed fields/verified bytes survive.
Migration validation and all 32 validator tests pass. No oracle expectations
were weakened to accommodate candidate behavior.

Reference provenance is the qualified immutable trusted publisher runtime in the
Docker baseline receipts, associated with source
`f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`. It is not an independently rebuilt
or signed attestation. The whole-service gate still needs the original 27 tests,
candidate quality, packaging and final aggregate review; this supplemental task
does not alone establish availability or authorize production selection.
