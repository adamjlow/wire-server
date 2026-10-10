# Offline comparison foundation

`compare.py` compares independently captured synthetic observations without any
network requests or shell execution. This is a foundation for the live runner;
it neither launches services nor proves that supplied captures came from them.
Every report says `parity_verified: false`, even when comparison succeeds.

Run `python3 -m unittest discover -s tools/parity -p 'test_*.py' -v` for self-tests.
Use `python3 tools/parity/compare.py manifest.json reference.json candidate.json report.json`
to produce a new report. Exit 0 means complete trace agreement, 1 means comparison
failed/blocked/skipped, and 2 means invalid input or an unusable report destination.
Existing reports are never overwritten. Inputs are bounded to 32 MiB and inline
bodies to 8 MiB. Larger streaming captures require a subsequent artifact protocol.

The tests provide schema examples. The manifest pins upstream/contract/fixture
revisions, the comparator's SHA256, both source/image/config identities, declared
provenance receipts, dependency images, clock/seed, isolated literal loopback
endpoints and required scenarios/steps/assertions. Captures identify their role,
language, source/image/config/oracle identity and each observation's result.
An unverified provenance declaration blocks every case. Declarations and passing
assertions in a supplied file still require independent verification.

Steps record the identical logical request (including explicit caller headers),
HTTP response, ordered side effects and required assertion outcomes. All response
headers are compared, with case-insensitive names and ordered repeated values.
Bytes are exact. Optional `json-object-order` permits only object member ordering;
numeric spelling, null/missing, bool/number, array order and duplicate members
retain meaning. Content lengths must match captured bytes except HEAD/204/304.
There are no configurable exclusions or broad body/header/URL normalizations.

Reports contain mismatch categories and input hashes, not capture bodies/header
values. Only synthetic input is permitted; the declaration is not an automatic
secret detector. Do not use production captures. Store captures separately with
the same review/privacy rules; no raw observations are published by this CLI.

Standalone comparator examples do not supply service qualification. Each service
needs authenticated live capture/image provenance,
service-specific complete fixtures, transport/gzip comparison decisions, validated
cargohold key/token/expiry/signature mapping, S3/federation effects and cross-runtime
storage switchback, operational thresholds, deployment selection and Haskell
build/default checks. Actual per-service reports and the
[current patched artifact qualification](../../docs/go-migration/qualifications/go-patched-20261009/README.md)
record completed evidence and limits; the offline comparator alone cannot satisfy
those gates.

## Automated entry point

Agents/CI run the checks and review evidence; the user need not manually test.
For proxy, one command builds identified candidate/provider images, runs actual
Haskell and Go processes in the existing internal synthetic Docker environment,
and requires the strict comparison to pass:

```sh
python3 tools/parity/harness.py proxy --profile api --output /absolute/new-artifacts
```

Use a clean committed checkout, Docker and exactly the version in
[go/.go-version](../../go/.go-version) on PATH (currently Go1.26.9). The harness
reads that central pin and rejects a different compiler before Docker/artifacts. `api` runs
103 live surface-area cases; `full` also runs real reload, operational/load-smoke
and switchback checks. Use API checks after API changes and full checks for a
relevant operational change/final handoff; metadata changes do not justify reruns.
A missing tool, failed capture or comparison exits nonzero and never writes a
passing result. Artifacts must be new and outside the checkout.

## Reuse original upstream e2e assertions

The original suite already launches `../../dist/<service>` in a service working
directory derived from the config path. The adapter stages this layout externally,
keeping every peer on Haskell and swapping only the selected service executable.
No Haskell test source or original `dist` binary is changed:

```sh
python3 tools/parity/harness.py prepare-e2e --service proxy --haskell-dist /absolute/haskell-dist --candidate /absolute/go-proxy --output /absolute/new-e2e-layout
```

`selection.json` contains exact argument vectors, working directories, original
binary hashes and required test names. Agents/CI execute those vectors in the
provisioned disposable upstream environment, retain both process exit statuses,
then run `check-e2e --service proxy --reference reference.xml --candidate candidate.xml`.
The checker requires every catalogued test on both runtimes, rejecting empty,
missing, extra, duplicate, failed or skipped results. It validates supplied XML;
it does not authenticate a run or replace direct differential/storage evidence.

Catalogue: five original proxy test functions (with all assertions inside them)
and the core cargohold API/version/token/upload/download/federation functions.
The five functions cover more assertions than five copied requests. Cargohold
storage switchback uses its service-specific evidence, linked from
[its review](../../go/services/cargohold/REVIEW.md); proxy cannot validate it.
The catalogue detects renamed/removed upstream tests before preparation.

**Environment status:** the [Docker harness](docker/README.md) now runs pinned
compiled Haskell assertions and peers with owned disposable dependencies. The
[current five-Go1.26.9 run](../../docs/go-migration/qualifications/go-patched-20261009/README.md)
passes 77 selected original tests, records every binary identity and records cleanup exit 0.
Preparation/preflight outputs still mean `e2e_executed: false`; only actual run
receipts establish execution. This is selected-suite evidence, not the entire
backend suite or qualification of background-worker's jobs/migrations.

**Stopping rule:** finite public contract/effect matrix, security negatives,
relevant error/retry cases, one operational smoke and switchback. Stop on that
matrix passing; extend it only for a concrete uncovered risk or regression.
Reuse previous passing evidence when its relevant inputs are unchanged. Preserve
normal Haskell CI, and run Haskell package checks when Haskell code changes.

## Docker e2e runner

The [container runner](docker/README.md) now builds from a pinned published Wire
integration image. Original compiled proxy/cargohold test discovery succeeds
inside a restricted non-root container; no host Haskell compiler is required.
The owned environment now provisions and qualifies backend dependencies and
static/dynamic peers without installing GHC/Cabal/Nix or backend packages on the
host. Use only the user-authorized internal Docker environment, with synthetic
data, trusted fixture TLS, no host ports and owned cleanup. Preflight alone still
cannot qualify e2e execution; shared/production integration targets remain prohibited.
Background-worker's [whole-service review](../../go/services/background-worker/REVIEW.md)
records its completed six-Go/Haskell original124, jobs/migrations, bounded operations
and actual durable switchback. Earlier failed baseline/construction runs remain history.

Spar's [whole-service review](../../go/services/spar/REVIEW.md) now records
original101 and the [current seven-Go/Haskell225 qualification](../../go/services/spar/qualification/evidence/final-original225-batched/result.json).
Six complete passing suites qualify in three owned namespaces with identical
selected artifact/config/runner/dependency identities; no failed XML is filtered
or merged and earlier failed overall attempts remain failed. Native protocol/trust,
exact-image loaded shutdown and durable native-Go-native state switchback pass.
See the Docker runner documentation for optional owned native-Nginx orphan
supervision; original assertions/deadlines/Haskell production code are unchanged.
