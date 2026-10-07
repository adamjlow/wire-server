# Executable plans

Before significant migration work, copy the
[plan template](../docs/go-migration/templates/service-plan.md) into the service
dossier (framework plans live in `docs/go-migration/plans/`). Link it from the
task. A plan is executable when another agent can run its steps and judge the
acceptance criteria without reconstructing a conversation.

Include the pinned reference revision, observed contracts, scope/non-goals,
dependencies, security requirements, small ordered steps, exact commands and
expected outcomes, evidence destinations, rollback and unresolved risks. Record
progress and decisions as work proceeds. Update the plan when discoveries change
it; scope/oracle changes require the corresponding task/review before coding.

Use short plans for narrow work. Do not duplicate current task status here or
maintain a second backlog in prose. Tests may be planned but not executed when
the environment is unavailable; leave the relevant gate unsatisfied.
