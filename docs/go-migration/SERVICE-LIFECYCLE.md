# Service lifecycle

Service state records use schema version 1. Advance one stage per reviewed update;
keep completed gate task IDs as the audit trail. Registration means only that a
service exists. Each gate task must belong to that service, be `done`, and have
passing evidence for every criterion plus an independent review.
Milestones describe development of an optional Go alternative. Haskell remains
supported, default and the reference at every stage; none authorizes its removal.

| State entered | Gate key / task kind | Evidence the reviewer must establish |
| --- | --- | --- |
| `registered` | none | Existing reference path and protected oracle paths recorded |
| `inventoried` | `inventory` / discovery | Entry points, dependencies, data, operations, tests and unknowns mapped to source |
| `contracted` | `contract` / contract-change | Versioned observable contract, baseline fixtures, security expectations, known gaps and human approval |
| `implementing` | `plan` / discovery | Executable implementation plan, toolchain decision, scoped tasks and baseline test strategy |
| `verified` | `parity`, `security`, `operations`, `rollback` / verification | Candidate builds/tests, differential parity, security regressions, operational compatibility, performance limits and rollback rehearsal all pass |
| `available` | `selection` / operations | Optional packaging documented and tested: omitted selection preserves Haskell, explicit per-service selection and supported mixed configurations work, unsupported choices fail clearly, data-compatible switchback passes |

Gates accumulate. The implementation plan is not candidate evidence; `verified`
requires the entire service contract, not just one endpoint. Gate task criteria
must enumerate the coverage and thresholds before execution. Unknowns and partial
parity keep the service at `implementing`. Non-applicable checks require a reason
and a separate approved contract decision; absence never means pass.

Current compatibility is separate from historical milestones. The nullable
`compatibility` field pins an upstream revision, Haskell reference revision, Go
candidate revision and a completed verification task. Entering `verified` or
`available` requires it to be populated. The verification report must establish
the checks in [PARITY.md](PARITY.md), including the unchanged Haskell defaults and
supported mixed deployments. Review verifies all revision and artifact identities.

An upstream, reference, candidate, fixture or configuration change that invalidates
evidence clears `compatibility` to null through a scoped coordination task. Keep
historical gates and done tasks intact. A new verification task can restore the
claim for the exact tested revisions without changing the historical stage or
modifying framework rules. An `available` service with null compatibility is not
currently advertised as a supported Go selection; existing deployments remain
pinned and require an operator's upgrade decision. See [upstream sync](UPSTREAM.md).

Production rollout and rollback are separate, per-deployment operations tasks.
Record chosen implementations, exact images/config, tested peer combinations,
operator approval, observation window and rollback evidence in a runbook. Two
deployments may choose different implementations indefinitely. Neither choice
changes the repository default or retires Haskell. Do not automatically switch
live traffic when a compatibility record changes.

The existing prohibition on integration runs remains in force. Preparing a parity
plan or runner interface does not waive it. Until an environment and policy are
explicitly authorized, integration parity evidence cannot be produced and the
`verified` gate cannot pass on that basis.
