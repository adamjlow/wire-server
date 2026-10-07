# Executable plans

Before significant migration work, reuse and update the existing service plan
when it covers the outcome. Create a new plan only for a distinct outcome that
needs one. Use the [template](../docs/go-migration/templates/service-plan.md) as
a checklist, not a requirement to duplicate every section for a small task.
Link the plan from the task. A plan is executable when another agent can run its
steps and judge acceptance without reconstructing a conversation.

Include the pinned reference revision, observed contracts, scope/non-goals,
dependencies, security requirements, small ordered steps, exact commands and
expected outcomes, evidence destinations, rollback and unresolved risks. Record
progress and decisions as work proceeds. Update the plan when discoveries change
it; scope/oracle changes require the corresponding task/review before coding.

Use short plans for narrow work. Do not duplicate current task status here or
maintain a second backlog in prose. Tests may be planned but not executed when
the environment is unavailable; leave the relevant gate unsatisfied.

Put the first runnable slice and direct reference comparison near the start of
service work. Reuse existing fixtures and tooling; add framework work only when
a specific validation need requires it. Include routine plan updates in the
outcome's existing task scope whenever possible, rather than creating a separate
planning task. Link existing security decisions and service-wide acceptance
requirements instead of copying them into every component plan.
