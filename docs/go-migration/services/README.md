# Service dossiers

The authoritative stage is in [`.migration/services/`](../../../.migration/services).
The initial registry covers proxy, cannon, cargohold, federator, gundeck,
background-worker, spar, brig and galley. All start `registered`; no service
inventory or Go compatibility is claimed.
Each record's `compatibility` is initially null. Haskell remains supported and
default; milestones never retire its implementation. Keep the latest verified
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
