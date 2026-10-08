# Gundeck qualification and delegated self-review

Approved by **codex (self-review delegated by user)**, the same actor as the
implementation author. This is not independent review. Authority is the user's
migration self-approval and autonomous-development instructions, recorded in the
[gundeck plan](../../../docs/go-migration/plans/gundeck.md). Approval covers the
finite supported matrix below; it grants no production switch or security exception.

Reference/upstream: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Qualified service source: `6b532d03cf2bb9e31a55b9108fa52abb79f05aea`.
Service binary SHA256: `a9d419826e320e9055c0cf87022a10338d07cacc60e70bbe494be4651b988f6a`.
Packaging/lifecycle source: `4d72e2061d7bffadb6f8ee79599c9371af41c3a4`.
Local image: `sha256:3c9311651c57c8a2b63976667f01183366a757b023c1546eee24a47dd1d02c42`.
The packaging change copies the exact qualified service binary and separately
builds the lifecycle helper. Both compiler receipts are in [image-build.json](evidence/final/image-build.json).

## Completed finite matrix

| Check | Actual result and durable evidence |
| --- | --- |
| Original Haskell control | Original gundeck40 passed against all-Haskell peers; [baseline](../../../docs/go-migration/services/gundeck/oracle/baseline.run.json) |
| Final original e2e | Proxy5 + cargohold27 + gundeck40 = **72 passed**, no skips/failures/errors; [run](evidence/final/final.json), original XML and run receipts alongside it |
| Frozen differential comparison | **91 passed**: unchanged HTTP/Redis38, Cassandra13 and native/provider/config/operations40; [comparisons](evidence/final/core.json), observations alongside it |
| Persisted switchback | Haskell writes Redis presence/Cassandra notification/native token/durable RabbitMQ queue; Go reads and extends them; Haskell reads Go state. Queue body/properties and cleanup verified; [switchback](evidence/final/switchback.json) |
| Stale delivery cleanup | Real Redis: old gone response preserves replacement generation/resource/client, while unchanged control is deleted; [generation](evidence/final/generation.json), reproducible [capture](generation_capture.py) |
| Operations | 40 requests/concurrency2: p95 0.653 ms, RSS 21,272 KiB, within predeclared bounds; clean idle shutdown and in-flight drain/exit0 in 0.514 s; native/switchback observations above |
| Go quality/security | Pinned Go1.26.8 format/tidy/verify/vet/race/unit/build/module vulnerability and strict license checks passed; [service checks](evidence/quality-final.log), [lifecycle/module checks](evidence/quality-packaging.log) |
| Optional image | Actual image UID65534/read-only/capDropALL/no-new-privileges, private owned network/no host ports, default config path, health200/SIGTERM0; [live image](evidence/final/image-live.json) |
| Existing Helm hook | Exact `sh -c "sleep 10"` delay executes successfully; other commands rejected without evaluation; [lifecycle](evidence/final/image-lifecycle.json) |
| Optional Helm selection | Actual upstream Helm3.22 render changes only gundeck image; omitted selection byte-identical, invalid shapes fail closed; unchanged [render proof](evidence/helm-selection.json) reused |
| Environment cleanup | All final owned dependency volumes/containers removed, process0/cleanup0; [environment](evidence/final/environment.json) |

The final original run uses Go proxy `0fae335b5cb5df7b7794bde68aea8177e327d3ef`,
cargohold `68a36b9137ce26566b2db91902469e8159885656`, cannon
`411f525e421d15e673870ace345462c4c4f80372`, and gundeck above, with the remaining
pinned Haskell peers. Exact peer hashes, immutable runner image/controller,
original test catalogue and fixture hashes are retained in the run receipts.
No Haskell service, shared original assertion, protected capture/normalization,
schema, default build, source chart or deployment default was edited by this task.

## Review findings and practical limits

Review compared source routing, principals/client IDs, paging/payload references,
Redis generations, queue properties, native sharing/replacement, SQS acknowledgements,
configuration defaults, compression and shutdown. Concrete fixes include Word64
canonicalization, strict UTF-8/single-value JSON, source JSON media requirements,
Redis redirected-node hostname verification, gzip response behavior, required
SQS QueueUrl, source 500 ms empty-poll default and required typed configuration.
Focused regressions passed; optional Go CI automatically discovers this module.

The [security notes](SECURITY-NOTES.md) record inherited concerns and the explicit
bounded-native-worker scheduling difference. Verified TLS is mandatory; deployments
requiring verification bypass retain Haskell. No unsafe parity exception was approved.
Provider tests execute synthetic SNS/SQS protocols and real owned dependencies;
this is not production AWS certification or a capacity/SLA benchmark. Native
Nix/GHC/Cabal and the legacy gundeck-integration executable are unavailable here:
full native Haskell compilation/unit tests were not run or claimed. Haskell sources
and existing CI remain unchanged.

Historical evidence remains honest. [core-5](evidence/core-5-failed/failure.json)
stopped on Python3.13 strict X509 incompatibility with the synthetic fixture CA;
the management check now uses the harness's existing CA/hostname-verified curl.
[final-1](evidence/final-1-failed/failure.json) retained a Haskell federator HTTP2
reset failure; a fresh run passed unchanged assertions. [final-2](evidence/final-2-previous/environment.json)
passed an earlier gundeck/cannon tuple and does not qualify the final tuple.
[final-3](evidence/final-3-aborted/abort.json) was deliberately stopped after its
receipt revealed that obsolete cannon selection. Only final4 qualifies the
current service binary and intended four-Go combination. No failures were
converted to passes and no protected oracle was weakened.

The finite acceptance matrix is complete. Haskell remains supported/default;
this approves optional availability for the exact tested tuple. No image/commit
was pushed remotely and no production rollout was performed.
