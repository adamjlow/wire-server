# Proxy completion and approach review

The optional Go proxy implementation is complete and ready for review at candidate
`76aefa94bd8739b96c3961e03bcec90ba27fe5ae`. Same-author review by
**codex (self-review delegated by user)** under the user's explicit request to
review contract preservation and self-approve when satisfied, followed by the
request to finish proxy before reviewing the approach. This is not independent
human review, a production rollout approval, or a claim of exhaustive equivalence.

## Evidence and decision

The [API report](comparison-final.json) passes **103/103** real Haskell-versus-Go
cases. It compares statuses, exact body bytes, application headers and ordered
duplicates, and provider requests/effects. Coverage spans every provider family,
methods, opaque/duplicate queries and credential handling, V0..V19 and invalid
versions, failure retries, redirects/loops, six-second provider delay, gzip,
HEAD, missing request IDs and structured/nested upstream errors. Narrow framing
normalization validates Date and lengths before allowing equivalent HTTP transfer
framing; it never ignores application values or failed requests. Historical
65-case captures remain available. [Raw observations](live-final.json) retain
source/image/compiler/binary/config/CA/fixture identities and cleanup evidence.

The [operational report](operations-final.json) passes application metric schema,
labels and counts; actual 60-second reload and invalid-update retention; disabled
versions; configured structured startup logging; zero-exit normal shutdown;
SIGTERM while both requests are demonstrably active, with both draining to 200;
and restarting the Go-selected endpoint with the pinned Haskell image and the
same configuration/secrets. Proxy has no owned persistent storage or queue.

The predeclared load smoke runs 40 forwarded requests per runtime at concurrency
four, all successful with exact bodies. Client-observed p95 including Docker
client startup: Haskell 0.086s, Go
0.058s. Go's reported memory after the batch is
7.316 MiB, below the existing chart's
50 MiB limit. This is a small regression smoke check, not a capacity prediction
or a peak-memory guarantee.

[Chart selection](selection.json) renders omitted overrides and switchback
byte-identically to the pinned upstream chart. Explicit Go repository/tag values
change exactly one image line; configuration, probes, service and Haskell peers
remain unchanged. Proxy talks to external providers, not other Wire service RPCs.
This does not claim execution of a full Kubernetes/mixed-backend deployment.
The [build receipt](build-final.json) and [CA provenance](build-final.ca.json)
identify the optional non-root scratch image and the same trust bundle as Haskell.
The image is local: it has not been pushed or deployed.

Pinned Go formatting/tidy/verify/vet/unit/race/build/vulnerability/license checks
passed at the candidate revision; no vulnerabilities were found. Packaging boundary
tests pass. The local AGPL and YAML MIT licenses were inspected; the scanner's
unpublished-module HEAD URL warning is documented. Fixture race tests, Python
boundary/comparator negatives, migration tests and whitespace/scope checks pass.
The comparison negatives reject changed statuses, bodies, headers, effects,
metrics, secrets, missing load samples, undrained shutdown and wrong rollback image.

**Approve the implemented proxy API boundary and this bounded evidence.** Do not
turn a comparator's coverage result into a blanket `parity_verified` assertion.
Any formal lifecycle promotion must name this candidate/reference and its real
coverage, and preserve the limitations below. Haskell remains supported/default.

## Limits that matter

- The reference is the pinned trusted-publisher Haskell image, linked to the
  source-evaluated image configuration; it is not an independent binary rebuild
  or a signed build attestation.
- Nix, Cabal and GHC are unavailable here. Haskell whole-project type checking and
  unit suites were not run. Haskell sources, libraries, integration tests, charts,
  original flake/lock and Cabal/Nix build inputs are byte-unchanged from the pinned
  upstream revision. Existing Haskell CI must still run in its proper environment.
- Native GC measurements keep names/units but have runtime-specific meaning:
  Go CPU is averaged over collections since the previous sample and wall time is
  the latest stop-the-world pause, rather than GHC's last-collection CPU/elapsed
  values. Application counters are compared exactly; GC numbers, latency buckets
  and live connections are validated structurally, not for numeric equality.
- The original Haskell integration executable was not run. Its request
  specifications were reused in the isolated runner. Full backend/cluster tests,
  HTTP proxy environment variants, pathological Configurator numeric syntax,
  every debug-log payload and production-scale load remain outside live coverage.
  Component/source checks are not relabeled as live execution.

## What to reuse and what to change next

Keep whole-service image substitution, unchanged Haskell defaults, pinned reference
identities and one shared suite exercised against both processes. Reuse existing
public Haskell tests first. Where their harness depends on the complete backend,
make the harness accept a selected service executable/image and provider fixtures;
keep its original assertions. For proxy we reused six request specifications,
not the original compiled assertions: that distinction should remain explicit.

The direct comparisons found valuable real differences in TLS protocol negotiation,
query bytes, upstream error wrapping, redirect headers, retry attempts, gzip and
raw-versus-buffered timeouts. These justify the small synthetic provider and strict
comparison rules. Reuse those tools and fixes for the next service rather than
repeat provenance discovery or create a new framework.

The first pass spent too long on preparation and status bookkeeping. For cargohold,
use the existing plan and infrastructure; get one upload/download round trip
running early against Haskell, reuse upstream assertions, then prioritize ownership,
tokens, expiry, federation, multipart/storage effects and rollback. Run focused
checks after relevant changes and one final evidence pass. Additional edge cases
need a concrete contract/security risk; repeated metadata builds and exhaustive
low-value permutations do not. Resolve cargohold's already-recorded security
contract ambiguity before implementing that affected boundary.

Cargohold has not started. The user requested this review after proxy; do not
begin it before that review.
