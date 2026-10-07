# Migration state, schema version 1

These JSON files are data, not executable instructions. The checked-in validator
in `tools/migration/check.py` defines the strict version-1 schema. Unknown keys
fail to catch typos. Templates live in `docs/go-migration/templates/` and are not
loaded as current work. JSON is intentional: no YAML parser dependency/coercion.

## Service records: `services/<service>.json`

| Field | Meaning |
| --- | --- |
| `schema_version` | Integer 1 |
| `service`, `reference_path` | Stable ID and existing Haskell service directory |
| `stage` | registered, inventoried, contracted, implementing, verified, available |
| `oracle_paths` | Nonempty literal paths protecting contracts, fixtures and reference material |
| `gates` | Gate name to nonempty list of completed task IDs; exactly the cumulative gates for the stage |
| `compatibility` | null, or `{upstream_revision, reference_revision, candidate_revision, verification_task}` for a verified Go baseline |

See the [lifecycle](../docs/go-migration/SERVICE-LIFECYCLE.md) for gate names,
required task kinds and substantive evidence. Gate IDs never authorize execution.
The Haskell reference directory must exist at every stage. `available` means opt-in
selection was demonstrated, never that Haskell is retired or a deployment changed.

Compatibility revisions are full lowercase 40-hex commit SHAs. The verification
task must be `done`, of kind `verification`, for this service, with every evidence
entry pinned to `candidate_revision`. Its report identifies all three revisions
and covers the [required matrix](../docs/go-migration/UPSTREAM.md). `--base` requires
compatibility when entering verified/available and rejects changing pins while
reusing the same verification task. A null claim can coexist with a historical
milestone when checks need refreshing; it advertises no currently verified Go
baseline. Clear and renew claims on affected updates without rewriting gate history.

## Task records: `tasks/<id>.json`

| Field | Meaning |
| --- | --- |
| `schema_version`, `id`, `title` | Integer 1, stable filename ID, bounded outcome |
| `service` | Registered service ID, or `framework` |
| `kind` | discovery, contract-change, implementation, verification, operations, framework |
| `status` | queued, active, review, blocked, done |
| `owner`, `branch` | null when queued; nonempty owner and unique branch/worktree identity when claimed |
| `depends_on` | Task IDs; no dangling references or cycles |
| `write_paths` | Nonempty exact files/directory prefixes, including this task record |
| `plan` | Existing repository file containing executable steps |
| `acceptance_criteria` | Nonempty list of `{id, description, required_evidence}` objects |
| `evidence` | List of `{criterion, result, command, revision, artifact}` objects |
| `review` | null, or `{by, artifact}`; done requires a reviewer other than the owner |
| `handoff` | Text; blocked tasks require a reason/recovery step |

Criterion IDs are unique. `required_evidence` describes what must prove that
criterion; `result` is pass, fail or blocked. `revision` is a full lowercase
40-hex commit SHA. Artifacts are existing repository files or HTTPS URLs without
credentials. A done task has evidence for every criterion, all passing, completed
dependencies, and review. Several evidence entries may support one criterion.
Use an artifact on a tested code commit followed by a separate evidence-record
commit; do not chase a self-referential "final SHA" in the task file.

Paths are relative, canonical, case-sensitive and literal, with `/` separators:
no absolute paths, `..`, globs, trailing slashes or symlinks. Directory prefixes
reserve descendants; task/service record files are reserved individually. Missing
future output paths are allowed in write/oracle scopes, but plans/artifacts must
exist. Global implementation-protected roots are `services`, `libs`, `integration`,
and `tools/migration`, plus migration policies/templates/workflow listed in
`PROTECTED` in the validator; per-service oracle paths add contracts and fixtures.
Active implementation tasks also require the service's `implementing` milestone.

## Checks and coordination

`make migration-check` validates the snapshot. `make migration-test` runs isolated
validator unit tests. `python3 tools/migration/check.py --base <commit>` additionally
compares git history with the working tree for legal transitions and immutable
completed tasks. Base must be a full commit SHA; CI supplies the PR base commit.
`--task <id>` with `--base` additionally checks actual file changes against scope.
No command field is executed and no artifact URL is fetched.

Use the [claim workflow](../.agent/TASKS.md). Current state is not a lock service:
merge claims centrally before parallel work, revalidate after rebasing and retain
history. Done records are immutable; corrections use follow-up tasks. Contract
changes and production operations require externally verified human review, not
just a populated JSON field. See [guardrails](../docs/go-migration/GUARDRAILS.md).

Rationale lives in docs decisions linked from plans/evidence; no parallel decision
database or generated status dashboard is maintained here.
