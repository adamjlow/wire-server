# Migration framework implementation plan

## Scope and findings

Build repository-local policy, knowledge, state and validation. Do not implement
a Go service, run integration tests, change runtime behaviour or deploy anything.
The initial working tree was clean.

Inspected the complete root `AGENTS.md`, root structure, `cabal.project`, root
Makefile, Nix entrypoint, GitHub workflow and contributor instructions, docs,
`hack/bin`, tools, services, libraries and integration suite. Existing conventions:

- Nix/direnv, Cabal with `-Werror`, treefmt, ormolu, and generated Nix dependencies.
- Unit tests run via `hack/bin/cabal-run-tests.sh`; integration executables need
  a provisioned environment and must not be run by agents under current policy.
- `integration/` tests public APIs without importing internal Haskell API types;
  `libs/wire-api/test/golden/` supplies existing serialization evidence.
- PR guidelines require a changelog entry. Copilot instructions delegate to
  root `AGENTS.md`. Existing CODEOWNERS remains intact.
- The checked-in GitHub workflow builds external wire-docs, not service parity.
  New migration docs are repository-local; publishing them needs wire-docs work.
- Python is available and included in the Nix environment. This execution host
  has no Nix, Cabal or treefmt; two existing submodules are uninitialized.

## Implementation

1. Refactor root policy while retaining Haskell commands, quality expectations,
   contributor links and concrete security requirements. Add Go-specific rules.
2. Document service boundaries, lifecycle gates, oracle protection, ownership,
   human approvals and a future differential runner interface. Add reusable
   inventory, contract, plan, decision and state templates.
3. Register the nine requested services at `registered`, with no invented
   inventories or completion evidence. Seed one unclaimed proxy inventory task;
   this is an illustrative starting point, not a mandated migration order.
4. Add a read-only, standard-library Python validator, focused negative tests,
   Make targets and a separate GitHub check. Validate shape, references, cycles,
   overlapping active write scopes, evidence completeness and lifecycle gates.
   Provide base-revision checks for transitions and optional per-task diff scope.
5. Run validator/tests, inspect diffs and links, and report unavailable checks.

## Deliberate adaptations

- Use `.json` records instead of YAML: strict parsing with Python's standard
  library avoids dependency drift and YAML coercion. Templates use the same form.
- Keep decision rationale in docs; service/task records link to it. No second
  `.migration/decisions/` store, empty directory scaffolding, Go module or runtime.
- Git review serializes claims; records are not a distributed locking service.
  A coordinator merges claims before dispatching agents into separate worktrees.
- CI validates records; it cannot certify that an artifact proves compatibility
  or that an approval was authored by a human. Protected review and branch rules
  remain maintainer responsibilities, clearly distinguished from automated checks.

## Acceptance and verification

Fresh checkout can run `make migration-check migration-test` using Python 3.10+.
Malformed/unknown state, bad paths, missing evidence, cycles, scope conflicts and
invalid state advances must fail. No service is marked migrated. Required
Haskell quality checks remain documented; unavailable tooling must be reported,
never represented as a pass. Record final results here after implementation.

## Security design

Use Python 3.10+ standard-library JSON parsing and subprocess argument lists, with
no new dependencies. Treat task text as data: do not execute commands, evaluate
expressions, fetch artifact URLs or launch services. Reject duplicate JSON keys,
unknown fields, non-canonical paths, symlink traversal, credential-bearing URLs,
incomplete evidence and overlapping claims. Use synthetic fixtures and temporary
repositories for negative tests. Git operations are read-only outside those test
repositories. CI uses read-only contents permission and does not persist checkout
credentials. This is a consistency validator, not a sandbox or approval authority.

## Initial verification results

- `make migration-check migration-test`: passed; nine registered services, one
  queued discovery task, 25 validator unit tests passed.
- `python3 tools/migration/check.py --base <current HEAD SHA>`: passed, including
  the bootstrap transition from a repository without migration records.
- Local Markdown link targets checked across all 19 framework/policy documents;
  all exist. `git diff --check` passed.
- Attempted the full build/unit command through `nix develop`, with the required
  output filter, and attempted `make lint-all` through `nix develop`. Both were
  blocked before execution by `nix: command not found`. Cabal and treefmt are also
  absent. Full Haskell type checking, unit tests, formatting/lint and generated
  Nix dependency checks are therefore unverified here. No Haskell source, Cabal
  manifests or Nix dependency definitions changed.
- No integration tests, Go service implementation, external account settings,
  production operations or invented completion/approval records were introduced.

The new GitHub workflow has not run remotely. Maintainers still need to configure
required checks/review protection on the fork; the committed workflow cannot
authenticate human approvals or prove that linked compatibility evidence is true.

## Follow-up plan: upstream-compatible, opt-in alternatives

The user clarified that Haskell must remain supported and default, upstream
updates must stay practical, and operators choose implementations per service.
Inspection found global cutover/retirement stages and a reference-removal
exception inconsistent with that requirement. The working tree still contains the
initial framework changes; preserve them and limit this update to framework files,
policy and its README/changelog entry.

1. Make unchanged Haskell defaults, additive Go support and small upstream diffs
   explicit in policy, architecture, templates and guardrails. Keep runtime builds,
   manifests and deployment behaviour unchanged in this session.
2. Replace cutover/retirement with an `available` milestone backed by default,
   selection and switchback evidence; require the Haskell reference at every stage.
   Production rollout remains a separate per-deployment operations decision.
3. Add nullable compatibility evidence to service records: exact upstream,
   reference and candidate revisions plus a completed verification task. Null
   means no currently verified Go baseline; immutable milestone history remains.
   Validate pinned revisions and task evidence without network access or commands
   from records. Test rejection of stale/relabelled evidence and reference removal.
4. Document an upstream-sync procedure, change-impact review, revalidation matrix
   and explicit default-Haskell fallback. No upstream fetch, merge, deployment or
   Go implementation is part of this change.
5. Run framework unit/state/transition checks and local documentation checks.
   Retain the documented limitations on Haskell tooling and integration execution.

This refines the initial, uncommitted schema version 1 before any service has
advanced; no released records or completed service evidence need migration.

### Follow-up verification results

- `make migration-check migration-test`: passed, with 32 unit tests. New coverage
  rejects global cutover/retirement states, missing Haskell references, malformed
  compatibility pins, mismatched candidate evidence and relabelled verification.
  It also exercises invalidating and restoring support without changing gate history.
- Base-revision transition validation, tracked/new-file whitespace checks and local
  links across 20 framework/policy documents passed. All nine service records remain
  registered with null compatibility; no availability or parity claim is invented.
- Haskell source/build/dependency/deployment files were unchanged by this follow-up.
  Nix, Cabal and treefmt remain unavailable, so the initial Haskell validation
  limitations still apply. No integration tests or upstream sync were executed.

## Branch placement

At the user's request, moved the uncommitted framework work from local `develop`
onto a new `go-migration` branch. Documented that as the fork's long-lived branch
and future PR target; `develop` tracks upstream separately. The framework push
check now targets `go-migration`, while PR checks remain enabled. No commits,
pushes, remote changes or upstream merges were performed as part of this move.
