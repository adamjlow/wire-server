# Initial live proxy comparison

Reference: Haskell source `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`, pinned trusted-publisher image in the existing oracle. Candidate: clean committed Go source and compiler/build receipt, identified by immutable local image ID (not a registry digest).

Scope: independently run the unchanged initial 65 stimuli against both processes; capture body bytes, all application headers and ordered provider effects. Extend the fixture client to retain parsed HTTP framing fields. Record transport Date/Server separately; validate Date syntax and run window, and Content-Length against the actual payload before narrowly allowing equivalent HTTP framing. Preserve Content-Type values/order, Location, request IDs and every other application header. Negative tests must demonstrate that status/body/header/effect changes and invalid framing fail. This is an initial coverage report, never a complete compatibility approval.

Security: Go 1.26.8 standard-library fixture, Python standard library, no new dependencies. Same authorized synthetic internal Docker environment; no published ports, egress, production data, privileged or host networking, disabled TLS verification or broad cleanup. Candidate cannot choose stimuli, normalization or expectations. Accept only validated local immutable image IDs and full source revisions, record exact binary/image/source/compiler identities. Reject symlink artifacts and existing output directories. Never retain test private keys.

Sequence:
1. Add candidate capture identity alongside the pinned reference, fixed internal client role and full framing observations; run unit/race and Python boundary tests.
2. Build the candidate from a clean pinned commit with CGO_ENABLED=0; record compiler/module/VCS info and local scratch image identity.
3. Run both isolated captures; compare every initial case including side effects. Keep raw observations and actionable discrepancies, with no compatibility claim.
4. Fix implementation discrepancies only within its separate implementation task, rerun changed candidate against this oracle. Review comparison rules using the user's explicit self-review delegation, clearly identifying same-author review. This delegation permits no behavior/security exception or production switch.

Commands: pinned PATH `python3 -m unittest discover -s docs/go-migration/services/proxy/oracle -p 'test_*.py'`; fixture `go test -race -count=1 ./...` with the required build-output filter; `capture.py --help`; new `compare.py --help`; `make migration-check migration-test`; scoped migration check at the claim commit; `git diff --check`.

Artifacts: oracle capture/comparison reports and this plan. Rollback: remove only this invocation's containers/network and optional fixture/candidate images; Haskell defaults and upstream inputs remain unchanged. Outstanding whole-service operations, full characterization, selection/switchback and Haskell checks remain separate requirements.

## Final risk-focused comparison

Extend the same independent runner with upstream request specifications, transport
failures, redirects/loops, response timeouts, gzip, HEAD, structured error data,
real secret reload/invalid retention, metrics, configured logging, disabled
versions, active-request SIGTERM drain and image switchback. Keep historical
captures; do not relabel their candidate/fixture revisions.

Before the final run, define a small load smoke test: 40 forwarded requests per
runtime, concurrency four, zero non-200 responses, exact synthetic response body;
candidate p95 client-observed latency no greater than max(1 second, twice the
reference p95), and reported candidate memory below the existing chart's 50 MiB
limit. This includes Docker client startup and is a regression smoke test, not a
production throughput/capacity claim. Existing integration executable/full stack
remain outside the authorized environment. Proxy makes no RPC to Haskell peer
services and owns no database/queue; image-only rendering preserves their choices.

Native runtime GC measurements retain metric schema/units but measure each
runtime's own heap and collector; do not assert numeric equivalence. Go reports
GC CPU averaged over cycles since the last sample and the latest stop-the-world
pause for GC wall diagnostics. This limitation must be visible in the review.
