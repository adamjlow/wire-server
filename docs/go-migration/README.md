# Go service migration experiment

This is an **experimental, AI-assisted migration**, with two aims: assess how
effectively AI can implement and validate compatible service replacements, and
compare Go with Haskell using equivalent workloads. Generated code, passing
tests and an optional image are evidence with a defined scope; they do not
automatically establish production readiness or a performance advantage.
There is no completed comparative performance study claimed here.

The experiment preserves the existing backend contracts and shared schemas.
Haskell stays supported and default, operators can choose Go per service, and
additive changes on the fork's `go-migration` branch keep upstream updates
manageable. Serious inherited security concerns are documented in service
reviews/security notes; changing existing behaviour is separate work.

Maintain upstream-compatible Haskell services and add optional Go alternatives at
whole-service boundaries. Haskell remains supported, default and the behavioural
oracle. Operators choose per service; no default changes or Haskell removal follow
from a Go rollout. Go implementations and opt-in image selectors exist for proxy,
cargohold, cannon, gundeck, federator, background-worker and Spar. See the
[current patched artifacts and validation](qualifications/go-patched-20261009/README.md).

| Where | Authority |
| --- | --- |
| [Root policy](../../AGENTS.md), [Go policy](../../go/AGENTS.md) | Operating rules |
| This directory | Persistent knowledge, contracts, plans and rationale |
| [`.migration/`](../../.migration/README.md) | Current service state and work queue |
| CI and reproducible test artifacts | Evidence; records only point to it |
| Haskell source at a pinned revision | Continuing reference behaviour, including for Go deployments |

Start with [principles](PRINCIPLES.md), [architecture](ARCHITECTURE.md),
[guardrails](GUARDRAILS.md), [lifecycle](SERVICE-LIFECYCLE.md) and
[parity](PARITY.md), then the [upstream-sync procedure](UPSTREAM.md).
Follow the [task workflow](../../.agent/TASKS.md) and
[plan rules](../../.agent/PLANS.md).

From the repository root run `make migration-check migration-test` (Python 3.10+,
standard library only). The validator reads records and reports failures; it
never executes recorded commands, claims tasks, launches services or deploys.
An `available` milestone means Go selection was demonstrated. Current compatibility
is recorded separately for exact upstream/reference/candidate revisions; a null
compatibility record means no verified Go baseline is currently advertised.

Consult [service records](../../.migration/services) for authoritative current
stages and exact compatibility pins. Cargohold, cannon, gundeck, federator, background-worker and Spar have
available optional implementations. Proxy is implemented, patched and tested, but
its formal stage remains implementing with no whole-service compatibility claim:
[review limitation](services/proxy/oracle/REVIEW.md). Historical receipts retain
their original meaning; the [current batched qualification](../../go/services/spar/qualification/evidence/final-original225-batched/result.json) passes225 unchanged original Haskell executions in six complete suites across three owned namespaces, with seven identical identified Go executables and remaining Haskell peers. Earlier overall failed runs remain failed and retained. The earlier [six-peer124](../../go/services/background-worker/evidence/final-original124-20261010/) and five-peer patched evidence retain their tested scope.

Background-worker is available as an optional whole-service replacement at its
recorded exact reference/candidate tuple. Its [plan](plans/background-worker.md)
and [final delegated self-review](../../go/services/background-worker/REVIEW.md)
link current original124, pinned Go checks, bounded operations, actual durable
Haskell→Go→Haskell recovery and optional image/selector proof. Historical failures,
inherited security concerns and unavailable native rebuilds remain explicit.
Spar is now available at its [exact compatibility tuple](../../.migration/services/spar.json).
Its [whole-service review](../../go/services/spar/REVIEW.md) links original101,
current batched225, native protocol/trust controls, exact-image loaded drain,
actual102-action durable switchback and optional image-only Helm selection.
Serious inherited concerns are [documented](../../go/services/spar/SECURITY-NOTES.md);
no Haskell code or security policy was repaired.

Choose subsequent services from dependencies, blast radius and testability;
no fixed migration order is implied. See the [dossier index](services/README.md).

The [framework plan](plans/framework.md) records repository findings and design
adaptations. Migration documentation is kept here for repository users; publishing
it to the external wire-docs site is separate work under existing docs conventions.

## Remaining scope

The nine native packages under `services/` match the
[migration catalogue](../../.migration/services). Eight have Go modules.
**Brig is incomplete and Galley has not started**; completing Brig precedes
Galley. Do not infer completion from a module directory or a passing subset.
Proxy's formal whole-service gate remains incomplete, and Federator's current
compatibility record is null despite its historical available milestone.
The service records and exact artifact receipts remain authoritative.

There is also a Haskell backoffice HTTP service,
[Stern](../../tools/stern/stern.cabal), outside that catalogue. A goal of
replacing every Haskell server would need to include it explicitly. Existing
Nginz gateway, external dependencies and database/schema/administrative utilities
remain separate scope; migrating the nine services does not remove all Haskell
from this repository.

## Go source layout

Each service has its own `go.mod`, with executables in `cmd/<service>/`.
A flat, cohesive package is reasonable for small services. Larger modules
(especially Brig, background-worker and Spar) should gradually extract private
packages under `internal/` around actual responsibilities: transport/routing,
authentication, client/account operations and storage adapters, as applicable.
Keep tests beside their packages, inject dependencies explicitly, and keep
interfaces small. Preserve contract/qualification evidence outside production
packages. Avoid a shared framework or exported `pkg/` tree without a concrete
consumer.

This follows the [official Go server layout guidance](https://go.dev/doc/modules/layout#server-project).
It is a recommendation for subsequent scoped refactoring, not a package
reorganization performed by this documentation change. Move one cohesive
boundary at a time and reuse the relevant existing checks.

## Proving compatibility without endless tests

Reuse existing automation before adding new suites:

- [Original Haskell integration suites in Docker](../../tools/parity/README.md):
  run unchanged assertions against selected Go executables with Haskell peers.
  Require the expected test names/counts and no failures, errors or skips. The
  current seven-Go evidence passes225 executions; Brig's final planned mixed
  stack requires535. Neither is a claim that all upstream tests have run.
- [Pinned Go checks](../../go/check.py): formatting, dependency verification,
  vet, unit/race tests, build and vulnerability/license scans per module.
- Existing service qualification controls: compare actual native/Go status,
  headers, bodies, authentication failures and side effects where original
  suites leave gaps; demonstrate Haskell→Go→Haskell reads/writes on the same
  real storage and exercise restart, retries and loaded shutdown.

Use one finite, declared matrix for the service surface and supported storage
configuration. Add a regression for an actual gap or mismatch. Rerun affected
checks after code changes and the complete required matrix at final handoff;
metadata-only edits do not warrant rebuilding or rerunning the backend.
Retain inherited native failures and unavailable checks explicitly, without
weakening assertions or repairing Haskell to obtain a pass. The user does not
need to perform these checks manually.

## Existing performance tools and comparison method

| Existing source | What it measures | Use for this experiment |
| --- | --- | --- |
| [MLS performance script](../../hack/bin/performance.py) | Large groups, member additions, sending and WebSocket reception; duration/percentile analysis | Reuse its external workload against native and Go stacks after checking current APIs, dependencies and synthetic fixtures. |
| [Cannon Criterion benchmark](../../services/cannon/bench/Bench.hs) | Concurrent dictionary insertion/lookup at different slice counts | Adapt the same workload to a Go benchmark for a focused implementation comparison. |
| [Gundeck Criterion benchmark](../../services/gundeck/test/bench/Main.hs) | Native-push serialization and thread-budget operations | Adapt equivalent inputs/operations to Go; report separately from end-to-end throughput. |
| [Spar operations probes](../../go/services/spar/qualification/operations.py) and [background-worker probes](../../go/services/background-worker/qualification/operations.py) | Finite concurrent request latency/counters and accepted-request drain | Reuse the container/process scaffolding for representative API workloads and resource sampling. Existing smoke results are not comparative benchmarks. |

The native benchmarks are declared as `cannon-bench` and `gundeck-bench` in
their Cabal packages. Their Haskell internals cannot benchmark Go directly;
equivalent workloads need Go benchmark adapters. The MLS script needs Python
packages and `mls-test-cli`; it uses API `/v4`, saves request/response material,
and its saved-path validation conflicts with the default absolute directory.
It is an inspected starting point, not a newly verified runnable benchmark.
Use only isolated synthetic data. Keep any adaptation separate from the
protected compatibility oracle.

For a fair comparison, use the same pinned peer images, schemas, reset dataset,
requests, TLS configuration and CPU/memory limits. Change one service at a time
first, then compare the complete qualified stack. Warm both implementations,
run a small fixed set of realistic concurrency levels, and alternate repeated
Haskell/Go runs to expose variance. Capture throughput, error rate, p50/p95/p99
latency, CPU time, peak/steady RSS and shutdown/recovery time, with exact
source/image/config identities. Measure startup/build time separately.

Start with proxy request forwarding, cargohold upload/download, and
Cannon/Gundeck notification delivery; add Brig authentication/account and
Galley conversation/MLS workloads after those services qualify. Health-only
probes cannot establish application performance. Database/S3/queue bottlenecks
and fixture limitations must accompany results. Publish measured results,
including regressions; do not assume either language wins.

The [experiment review](plans/experiment-review.md) records the inspected
sources, scope and documentation validation.
