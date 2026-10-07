# <service/task> executable plan

Task: <id>; contract: <path/revision>; reference: <full SHA>.

## Objective and boundaries

State outcome, non-goals, exact write paths, dependencies and claim owner.
Explain idiomatic Go design choices without mechanically porting Haskell.
List unavoidable edits to upstream-owned files and how to minimize them. Preserve
Haskell defaults and optional Go tooling; record the imported upstream SHA and
how updates invalidate/reverify candidate support. See [upstream sync](../UPSTREAM.md).

## Security and alternatives

Specify input/trust boundaries, validation, resource bounds, error handling,
dependency/toolchain versions and secrets handling. Compare meaningful alternatives
and justify the chosen approach. List decisions requiring separate review.

## Steps

1. Establish a reproducible baseline and record exact reference/config/fixture hashes.
2. Implement small increments, each with exact commands and expected outcomes.
3. Run permitted checks, collect artifacts, update discovered constraints.
4. Hand off for independent review with remaining risks and rollback instructions.

## Acceptance and evidence

For each task criterion specify its command/procedure, pass condition, artifact
destination and environment. Include Haskell checks when affected, Go checks when
available, and parity/security/operations as appropriate. Integration checks need
explicit authorization under root policy; blocked is never pass.
Include unchanged all-Haskell workflows, per-service opt-in selection, the supported
mixed-version matrix and data-compatible switchback. Link the compatibility report
with exact upstream/reference/candidate revisions before advertising Go support.

## Progress, rollback and handoff

Record completed steps with evidence, unresolved questions, abort conditions and
recovery procedure. Keep current task state in `.migration/`, not a duplicate queue.
