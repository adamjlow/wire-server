# Go service migration

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
