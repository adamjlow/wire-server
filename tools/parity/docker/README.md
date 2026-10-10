# Containerized upstream e2e runner

The runner keeps the compiled Haskell suite, Wire service executables, native
libraries and Python runtime inside Docker. Nothing installs GHC, Cabal, Nix,
Cassandra, RabbitMQ or S3 tooling on the host. The base is Wire's published
integration image, pinned by immutable digest (release 5.37.5); this is trusted
publisher provenance, not an independently rebuilt/source-attested binary.

From a clean committed checkout:

```sh
python3 tools/parity/docker/control.py build --output /absolute/new-build.json
python3 tools/parity/docker/control.py preflight --receipt /absolute/new-build.json --output /absolute/new-preflight.json
```

The first pull downloads the published runtime bundle (about 3.9 GB unpacked).
Subsequent builds reuse it and add only the small runner scripts/catalogue. The
builder uses a temporary Docker context and `--network=none` for build commands;
no repository-sized image copy or local Haskell compilation is needed.

The controller records the local immutable runner image, source revision and
input hashes. Its preflight creates an owned container with networking disabled,
read-only rootfs, non-root UID 65534, all capabilities dropped, no-new-privileges,
bounded CPU/memory/PIDs and a small writable tmpfs. Cleanup uses only the actual
created container ID. It checks the compiled integration/service binaries and
lists five original proxy and 27 core cargohold test functions. All assertions
inside those original functions are retained; tests are not translated into Go.

`preflight-pass` explicitly means `e2e_executed: false` and
`environment_ready: false`. It proves the published executables/test catalogue
are usable in Docker, not that backend dependencies or e2e assertions passed.

## Automated private baseline and candidate runs

Build the runner once, then provision and run a selected service's original tests:

```sh
python3 tools/parity/docker/control.py build --output /absolute/new-build.json
python3 tools/parity/docker/environment.py prepare --receipt /absolute/new-build.json --output /absolute/new-environment
python3 tools/parity/docker/environment.py run --output /absolute/new-environment --service cargohold
```

`prepare` records configuration hashes and a unique project; `run` resolves the
upstream test dependency images to immutable digests, starts private internal
networks with no published ports, initializes synthetic schemas/resources and
starts the pinned Haskell peers. The original compiled suite starts its dynamic
peers itself, from a temporary service/dist layout inside the container. Reports
and logs are retained outside the checkout. The controller removes only its
project's containers, networks and volumes after execution. Commands are bounded;
missing/failed/skipped cases and nonzero exits cannot pass. The expensive setup
and original tests are executed automatically; the user is not the test runner.

To select one Go service, add `--candidate /absolute/service-binary
--candidate-receipt /absolute/build.json` to **prepare**, then run the selected
service as above. The build receipt must identify the binary SHA-256 and full
source revision. The controller checks its checksum before starting, mounts it
read-only and replaces only that service in the temporary dist layout for both
static and dynamic peers. All other executables remain Haskell. Use a new output
directory for each attempt and a clean committed checkout. Source revisions,
runner/dependency images, fixture hashes, candidate hash and complete XML/run
reports are recorded; a publisher image is not independent source attestation.

The same image supports standalone `suite --service proxy|cargohold --config FILE
--output FILE [--candidate FILE]` in a separately provisioned private environment.
Configs outside `services/` use the upstream PATH launch mode and `/etc/wire`
layout. The controller uses the upstream local service/dist mode instead. Do not
run the original development Compose file unchanged: its global names and host
ports are inappropriate for this disposable setup.

The fixture Redis cluster and CoreDNS use upstream private advertised IPs. The
controller rejects overlap with existing networks; run one stack at a time on a
Docker daemon. Search data uses bounded memory-backed storage to avoid the host's
Elasticsearch disk watermark. The first image pulls need several GB (runner
about 3.9 GB plus dependencies about 4 GB); keep at least 2 GiB free for test data.
No GHC, Cabal, Nix or backend packages are installed on the host. The enterprise
peer fixture comes from its public chart because the private submodule is not
available; the actual published Haskell executable runs. These selected tests do
not qualify unrelated enterprise, dynamic DNS administration or OpenSearch paths.

## Actual qualification

[Retained baselines](baselines/) record all five original proxy and 27 selected
core cargohold tests passing on Haskell, then the same five proxy tests passing
with the identified Go proxy and Haskell peers. The fixtures match across runs;
all owned stacks were cleaned up. Baselines used controller revision
`603570565533a2f03ea100399825d0799bf199e6`; Go selection used
`c75686850f4d92c13a41e311b86bc861e8b86271` and Go proxy source
`0fae335b5cb5df7b7794bde68aea8177e327d3ef`. Exact runner image/input provenance
is in the two environment build receipts. This qualifies original-test reuse and
service selection. It does not claim Go cargohold compatibility or complete
coverage of any future service's contract; supplement the selected suite only
for concrete gaps in that service's finite acceptance matrix.
# Combined optional services

Use repeated `prepare --select SERVICE BINARY BUILD_RECEIPT` arguments for
identified binaries. Available combined profiles are `proxy-cargohold`,
`proxy-cargohold-cannon`, `proxy-cargohold-cannon-gundeck`, and
`proxy-cargohold-cannon-gundeck-federator`; pass the matching profile to `run --service`.
Every service in the selected profile requires exactly one identified selection.
For example, `run --service proxy-cargohold` starts both Go components simultaneously, keeps all other backend service
executables from the pinned Haskell image, and executes the unchanged original
5 proxy and 27 cargohold tests in the same disposable environment. Both suite
receipts record every staged service executable's implementation and SHA256;
the host checks both selected hashes in both reports before accepting success.
Missing/duplicate/unsupported selections and changed binaries fail closed.
This bounded combined run does not claim the full backend suite was executed.

The current [patched five-service run](../../../docs/go-migration/qualifications/go-patched-20261009/README.md)
passes 5 proxy + 27 cargohold + 40 gundeck (including 35 cannon) + 5 federator = 77
original Haskell tests with five Go1.26.9 executables and remaining Haskell peers.
All four suite receipts identify every selected executable; the host verifies
hashes before accepting success. Exact runner, controller, image and fixture
identities remain in the retained receipts. No complete backend-suite claim.

Background-worker was a Haskell peer in the historical five-service run. Its
[whole-service review](../../../go/services/background-worker/REVIEW.md) records
completed original124 with six Go services, bounded operations and actual durable
switchback. Earlier partial/failing construction runs remain retained, not a
passing native baseline. Existing event coverage alone did not qualify jobs/migrations.

## Spar original parity profile

`--service spar` runs101 unchanged original executions:86 Spar module cases plus15 related cross-service SSO/SCIM cases from actual pinned discovery. The explicit spar-tests.json catalogue retains all original boolean variants, assertions and required XML checks. Use prepare without selections for an all-Haskell baseline; add an identified Spar candidate or repeated --select arguments for substitution.

`proxy-cargohold-cannon-gundeck-federator-background-worker-spar` requires exactly seven identified Go candidates and runs the existing124 peer executions plus Spar101 (225 executions). Missing, duplicate, foreign or ambiguous selections fail; source/fixtures/candidate mounts remain read-only and dependencies/cleanup remain owned. Adding a profile does not claim a suite passed or change Haskell defaults.

The [current whole-service qualification](../../../go/services/spar/qualification/evidence/final-original225-batched/result.json)
passes all225 executions through six complete original suites in three owned
batches: proxy5/cargohold27, Spar101/gundeck40/federator5, worker47. All seven Go
artifact hashes and Haskell remainder/config/runner/dependency identities match;
strict original XML/names and owned cleanup pass. Earlier overall failed runs
remain failed, no partial failed suite qualifies and no synthetic XML is merged.
The final worker47 used optional orphan supervision with original exit0 and zero
supervisor signals; the separate native proof demonstrated actual orphan reaping.


## Optional native Nginx orphan supervision

`environment.py run --reap-orphan-nginx` runs a nonroot PID1 supervisor inside the existing disposable runner. It terminates only adopted native Nginx workers left after the published test runner kills their master after50ms; healthy workers and Go services remain untouched. Original assertions, catalogues, timeouts and selected hashes are unchanged, and an original failure remains a failure. Actual native orphan/healthy-worker proof and host-guard/harness tests are retained in [the qualification](../../../docs/go-migration/qualifications/e2e-nginx-orphans-20261010/qualified/). The option is test-environment housekeeping, not a Haskell source or production shutdown repair.
