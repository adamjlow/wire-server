# Service dossiers

The authoritative stage is in [`.migration/services/`](../../../.migration/services).
The initial registry covers proxy, cannon, cargohold, federator, gundeck,
background-worker, spar, brig and galley. Current records, rather than this index,
provide live stages and verified revision tuples. Existing dossiers cover proxy,
cargohold, cannon, gundeck, federator and background-worker; their evidence
includes real Haskell reference captures and original tests.

Use [current patched artifacts](../qualifications/go-patched-20261009/README.md)
for the Go1.26.9 tuple; previous compiler receipts remain historical. Proxy's
[formal operational limitation](proxy/oracle/REVIEW.md) remains unresolved despite
its implemented/tested API and patched image. Background-worker is available at its
[exact compatibility tuple](../../../.migration/services/background-worker.json).
Its [inventory](background-worker/inventory.md), frozen
[contract](background-worker/contract.md), [plan](../plans/background-worker.md)
and [final self-review](../../../go/services/background-worker/REVIEW.md) link
current six-Go/Haskell-peer original124, operations, durable switchback and optional
selection evidence. Historical failed reference/candidate runs remain retained;
no original assertion was waived. Spar, brig and galley remain unimplemented.

Haskell remains supported and default; milestones never retire its implementation. Keep the latest verified
upstream/reference/candidate tuple and its report in the compatibility record,
following [upstream sync](../UPSTREAM.md) when new changes arrive.

On the first discovery task, create `services/<name>/inventory.md` in this
directory from the [inventory template](../templates/service-inventory.md).
Then add `contract.md`, `plan.md` and decisions only as needed. Persist source
references, unknowns, evidence links and handoffs here; avoid placeholder dossiers
for work nobody has done. `contract.md` and any `oracle/` directory are protected
by default. Register additional oracle paths before implementation starts.

The service JSON links to its actual reference source today and later gates link
to completed tasks, whose plans and evidence locate the resulting knowledge.
