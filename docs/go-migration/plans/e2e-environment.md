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
identities, no socket/host mounts or published ports, no privileged containers,
verified test TLS, bounded startup/test time and memory, own cleanup only.
Read-only original source/fixtures, separate writable temporary runtime/reports.
Do not reset shared databases. Reject missing/failed/skipped test reports.
Existing dependency versions are test-only upstream fixtures, not recommendations
for production. Disk has 8.5 GiB free initially; inspect and fail before exhaustion,
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
