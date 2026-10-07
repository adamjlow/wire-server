# Proxy and cargohold execution plan

Goal: provide optional, whole-service Go alternatives for proxy and cargohold,
with repeatable like-for-like comparison and data-compatible switching back to
Haskell. Haskell defaults and upstream workflows remain supported.

Inspected source baseline: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Framework baseline: local commit `5f08bdd75` on `go-migration`. Current preparation
branch: `proxy-cargohold-preparation`; future task PRs target `go-migration`.

## Scope and sequence

1. Complete source-cited inventories for both services. Draft contracts from the
   existing implementation, routes, config, middleware and test observations.
   Record uncertain runtime details for differential characterization.
2. Independently review inventories/contracts before candidate implementation.
   Request one concrete human review covering both source-derived contracts and
   the isolated parity environment, with no intentional behaviour exceptions.
3. Pin an optional Go toolchain and module/dependency versions. Add explicit
   candidate check commands and CI, preserving default Haskell targets. Build an
   independent runner and deterministic fake provider/Brig/S3/federator fixtures.
4. Establish immutable Haskell reference images and source provenance. Current
   Docker works; native Nix/Cabal/GHC do not exist. Published reference images
   must be pinned by digest and tied to source, or clearly treated as an alternate
   baseline until source provenance is established. Do not pretend a release tag
   alone proves it matches this checkout.
5. Implement proxy first, preserving all provider routes, query/header handling,
   retries, secrets reload, versions, metrics and shutdown. Unit/race/security
   tests precede the approved isolated differential run.
6. Implement cargohold's full service boundary: V0/V1 and V2+ APIs, user/bot/provider
   principals, exact asset/object/metadata formats, multipart streaming, tokens,
   S3/CloudFront/multi-ingress redirects, legacy assets, federation, audit and metrics.
   Preserve upstream schemas; never replace S3 with an in-memory production store.
7. Compare both implementations with the same scenarios and equivalently seeded
   isolated dependencies; test deliberately corrupted candidates to verify the
   comparator detects mismatches. Produce versioned reports with source/image,
   fixture/config and comparator identities, coverage and per-scenario outcomes.
8. Add explicit deployment selection, verify omitted selection remains Haskell,
   and exercise Haskell→Go→Haskell against a shared test S3 store. Complete service
   gates only after their evidence and independent review actually exist.

## Ownership and security

Preparation tasks write dossiers and their own records. Contract tasks own
assertions/fixtures; candidate tasks own Go service code and unit tests only.
Toolchain/runner/packaging use separately scoped shared-file tasks. Read the
reference broadly; never modify it or loosen a comparator to pass a candidate.

Use Go 1.26.8, a currently supported patch release verified against the official
download API, with archive checksums. Prefer maintained YAML parsing and AWS SDK
v2 over custom parsers/signing. Review vulnerabilities and licenses for pinned
versions before use; keep Go dependencies out of Haskell's default environment.

Parity uses synthetic credentials/data, task-specific Docker containers and
temporary directories, loopback-bound endpoints, fake providers/Brig/federator,
and isolated S3 test stores. No live provider calls, production endpoints or
existing shared databases/containers. TLS verification remains enabled; test
TLS uses a generated trusted CA rather than an insecure client. Bound input,
timeouts and memory, redact sensitive query/header values, and preserve reviewed
external errors. Existing full integration suites remain prohibited by root policy.

## Acceptance and evidence

- Each service retains its existing external/config/storage contracts and all
  declared scenarios pass against the pinned Haskell reference. Missing coverage,
  uncertain source provenance or unrun checks prevent availability claims.
- Candidate modules pass formatting, vet, unit/race/security tests and dependency
  checks. Runner unit tests prove normalization retains meaningful differences.
- Existing Haskell build/unit/default deployment checks pass in a provisioned
  environment; otherwise record that gate as blocked.
- Reports record upstream/reference/candidate SHAs and image digests, fixtures,
  configuration, supported versions, commands and mismatch artifacts. Passing
  self-tests or mock-vs-mock runs are never recorded as Haskell-vs-Go parity.
- Default deployment rendering is preserved; explicit choices select only the
  requested service. Switchback preserves Go-written objects, metadata and tokens.

## Current preparation findings

- Framework has 32 passing validator tests and a local bootstrap commit; it has
  not been pushed and remote CI has not run.
- Docker Engine is usable. Verified Go 1.26.8 is installed in a task-specific user
  cache. No system-wide Go/Nix installation or default Haskell change was made.
- Published proxy 5.37.4 image was downloaded by digest for provenance inspection;
  it has no source-revision label. No integration/parity test has been run.
- Native Haskell tooling remains absent; available disk is approximately 17 GiB,
  so a full uncached Haskell/Nix build cannot be assumed feasible.

Update this plan with actual task/evidence links as work proceeds. Unavailable
checks remain explicit blockers, and deployment/runtime changes require their own
operator-approved operations task.
