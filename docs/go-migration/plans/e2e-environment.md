# Private Docker e2e environment

Complete the Docker setup requested on 2026-10-08 before creating the requested
Go cargohold goal. Reuse the pinned published integration runner and original
compiled tests: five proxy and 27 core cargohold functions. Do not rewrite or
weaken their assertions. Original source reference is
f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; publisher provenance and known source
attestation limits remain as recorded in the runner plan.

Use upstream ephemeral dependency configuration as input, strip global names,
host ports and unrelated dashboards/demo services, and create a unique private
Compose project with internal networks and owned volumes. Render fixture paths
and static A/B peer configurations from the original service configs and
Testlib.RunServices/ModService definitions. Published executables supply all
Haskell/native tooling. Host requirements are Docker, Compose and Python stdlib.
Use original schema and resource initialization commands against only this stack.
Run Haskell first, then the same runner can substitute exactly one service binary.

Security requirements: fixed argv, synthetic fixture credentials, immutable image
identities, no Docker socket or host system mounts or published ports, no privileged containers,
verified test TLS, bounded startup/test time and memory, own cleanup only.
Read-only original source/fixtures, separate writable temporary runtime/reports.
Do not reset shared databases. Reject missing/failed/skipped test reports.
Existing dependency versions are test-only upstream fixtures, not recommendations
for production. Disk has 8.5 GiB free initially; inspect and reserve 2 GiB for disposable data and fail before exhaustion,
never prune unrelated workloads. Rollback removes only this generated project.

Steps: claim; build private stack renderer/controller and fixture/runtime adapter;
validate isolation before starting; pull and identify required images; start owned
stack and wait for readiness; initialize resources/schemas; start original peers;
execute original proxy and cargohold suites and retain complete XML/run receipts;
review focused checks and actual evidence. Only successful original baselines
qualify the setup. Then create the user-requested Go cargohold goal. Full Haskell
source builds remain unavailable locally and no source changes are planned.

Use `python3 tools/parity/docker/environment.py --help` for the controller;
`python3 -m unittest discover -s tools/parity -p 'test_*.py'` and
`make migration-check migration-test` for focused checks. Runtime qualification
is required in addition to these unit tests. Same-author review is delegated by
the user; annotate it explicitly. No production or security exception approval.

## First runtime attempt

At 484b1bd5e611a3ca9884c731c9bf88b266fe9d3a all required dependency images
resolved, the private stack and TLS Redis cluster started, and original Haskell
Cassandra schema migration commands executed. No original test assertions ran.
Elasticsearch detected the host filesystem at 98% use and set its flood-stage
read-only watermark. Stopped only the owned runner and retained failure/logs;
controller cleanup removed its project containers/networks/volumes. Use a bounded
1 GiB tmpfs for disposable search data on retry, retaining the 2 GiB host disk
reserve for other backend data. No unrelated images, caches or services removed.

The next complete setup at b6673169728cb45d6c2e0319a582044453df73e8
initialized schemas, AWS fixtures, Postgres migrations and all static Haskell
peers successfully. The original integration process stopped at federation
warmup because CoreDNS's upstream SRV target `localhost.` did not resolve through
Docker's internal-network resolver. Add the exact loopback hostname to the owned
runner's hosts file; retain original DNS SRV records and federation assertions.
No selected assertions passed and no compatibility claim follows from this run.

## Qualified outcome

All five original proxy and 27 selected core cargohold functions passed on
Haskell at controller revision 603570565533a2f03ea100399825d0799bf199e6,
using runner image ed51ca479409d6a633f07a7a7bc44758c21083670d8a913217f20318dbffde36
(runtime source 484b1bd5e611a3ca9884c731c9bf88b266fe9d3a). Go selection at
c75686850f4d92c13a41e311b86bc861e8b86271 passed the same five proxy functions
with Go proxy source 0fae335b5cb5df7b7794bde68aea8177e327d3ef, binary
7868aa4b6b914daeb65a8033506dc03963af4ba34b37f0ef0bf4e2b2e7001238, and
Haskell peers. Only the selected service is replaced for static and dynamic
backends. The three fixture hashes match. Retained complete XML, process exits,
images, fixture/candidate hashes and successful cleanup are in
[baselines](../../../tools/parity/docker/baselines/). Build receipts retain the
actual runtime inputs; do not relabel the older Haskell baselines as newer runs.

No source Haskell, original test, fixture, chart, Nix or Cabal inputs changed.
47 focused harness/environment tests and 32 migration tests pass. Local full
Haskell compilation/units remain unavailable, not passed; original upstream CI
remains intact. No Go cargohold implementation, complete contract equivalence,
production switch, source attestation or security exception is claimed. Existing
cargohold security findings still need explicit contract disposition before
those boundaries are implemented or full compatibility is claimed.

Review: **codex (self-review delegated by user)** under the user's explicit
self-review and trusted autonomous-development instructions. Verified complete
original reports, fixture equality, selected Go executable identity, pinned
Haskell peers, private networks/no published ports, synthetic fixtures and
cleanup of only owned resources. Approve this bounded environment outcome;
retain service-specific compatibility gates and unresolved contract findings.
The next requested goal is building optional Go cargohold using this harness.

Final controller review ensures log collection timeout cannot skip owned-stack
cleanup and unsuccessful cleanup cannot return a qualified pass. A focused
injected-timeout check exercises that security boundary. Original suite results
are reused for unchanged runtime/fixture inputs; documentation and this cleanup
error-path check do not justify rebuilding Haskell or repeating passed suites.
