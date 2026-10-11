# Experiment documentation review

Requested outcome: explain the AI-assisted Go experiment, audit remaining
services, review layout, identify existing correctness/performance tools and push
current work to the fork's `go-migration` branch.

## Bounded procedure

1. Inspect the native service Cabal packages, executable tools and service registry.
2. Inspect Go module/command/package layout and official Go module guidance.
3. Inspect existing original-suite Docker runner, parity/operations tools and
   native benchmark sources. Record tested scope and proposed adaptations separately.
4. Update migration and Go entry-point READMEs; leave runtime, fixtures, selectors
   and service compatibility records unchanged.
5. Validate local links, task scope and migration records/tests; review the
   documentation under the user's delegated self-approval instruction. Commit
   and push normally to `origin/go-migration`, then verify remote HEAD.

## Audit findings

The nine packages under `services/` match the nine migration service records.
Eight have Go modules; Galley has none. Brig remains implementing and incomplete.
Proxy has executable qualification but remains formally implementing; Federator
has an available milestone with a null current compatibility claim.
Stern is an additional Haskell HTTP backoffice facade: `tools/stern/stern.cabal`
defines the `stern` executable. It has no Go module or migration record.
Database/schema executables are separate tools, not automatically service scope.
Nginz is an existing Nginx-based gateway and remains unchanged.

All eight modules already separate commands under `cmd/`. Root Go-file counts
(including tests) at inspection: proxy12, cargohold17, cannon11, gundeck21,
federator5, background-worker142, spar63, brig142. Counts indicate scale,
not proof of poor cohesion. Recommend incremental private packages at actual
boundaries for the larger services; no blanket package shuffle in this task.
Official source: https://go.dev/doc/modules/layout (server-project guidance).

Existing tools inspected: `tools/parity/README.md`, the Docker original-suite
runner, `go/check.py`, Spar/background-worker qualification operations,
`hack/bin/performance.py`, `hack/python/wire/mlscli.py`, Cannon's Criterion
benchmarks and Gundeck's Criterion benchmarks. The MLS script is a candidate
workload requiring dependency/API/fixture validation; it was not run here.
It uses `/v4`, logs request/response material, and its path validator rejects
absolute saved paths despite the default `/tmp` directory. Use isolated synthetic
fixtures and report these inherited limitations; do not repair Haskell or this
script as part of documentation. Microbenchmarks are not API parity or
whole-service speed comparisons. Operations probes are bounded smoke tests,
not a completed comparative benchmark campaign.

## Validation and review

Results and tested revision are recorded in
[the documentation task](../../../.migration/tasks/experiment-documentation.json).
Review is by the same author under the user's earlier explicit instruction
to review and self-approve; it is not an independent reviewer. No service
qualification claim is created by this documentation review.
