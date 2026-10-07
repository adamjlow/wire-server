# Keeping the fork compatible with upstream

The fork tracks wireapp/wire-server and adds optional Go service implementations.
Haskell remains the supported default and behavioural reference. An upstream
update must not require operators to adopt Go or delay Haskell fixes until a Go
candidate catches up. A lagging candidate loses its current compatibility claim;
an existing deployment stays pinned until its operator chooses an upgrade.

## Branch model

- `origin` is the fork, `adamjlow/wire-server`. Publish our work there.
- `upstream` should point to `wireapp/wire-server` for fetching changes; confirm
  or configure that remote before syncing. Creating a local branch does not add it.
- Keep the fork's `develop` aligned with upstream `develop`, without Go-programme
  commits. Do not merge our feature branches into `develop`.
- `go-migration` is the long-lived fork branch containing optional Go support and
  this framework. Create task branches from it and target PRs at the fork's
  `go-migration` branch. The initial framework can be committed directly on this
  branch as its bootstrap; subsequent work uses task branches and review.
- Bring upstream updates into a dedicated sync branch based on `go-migration`,
  then review a PR back into `go-migration`. Record exact upstream SHAs as below.
  Fast-forward the fork's `develop` independently when appropriate; stop and inspect
  divergence instead of resetting or force-pushing shared history.

This separates our development from the upstream tracking branch. It does not
remove the need to resolve upstream conflicts or revalidate compatibility.

## Keep the difference small

Keep Go implementation/tooling under `go/` and programme knowledge/state in their
existing directories. Extend existing packaging with small, explicit selection
points; do not copy Haskell services or whole chart/build trees. Preserve original
names, defaults, existing values-file semantics and build targets. Go tools belong
in optional development/build paths, not the default Haskell dependency closure.

Keep fixes useful to upstream separate from Go implementation commits so they can
be proposed upstream independently. Record unavoidable changes to upstream-owned
files in service plans with rationale and source references. Avoid unrelated
formatting/refactors and regenerate generated files with upstream's own tools.

## Synchronization procedure

1. **Prepare a bounded sync task.** In a clean worktree inspect remotes and confirm
   which one points to wireapp/wire-server; do not assume `origin` is upstream or
   guess the tracking branch. Record the previous imported upstream SHA, intended
   upstream branch/release, current fork SHA and outstanding local patches in the
   task's plan. Coordinate shared-file ownership before merging.
   Use a separately reviewed `contract-change` task when the sync changes reference
   behaviour or oracle material; keep candidate adaptations in their own tasks.
2. **Fetch and integrate deliberately.** When performing an authorized sync, fetch
   that remote, pin its full target SHA, and merge it on a dedicated branch. Prefer
   a merge that retains provenance on shared branches; do not force-push or rebase
   other agents' published history. Resolve conflicts preserving upstream Haskell
   behaviour and defaults. No upstream sync is executed by this framework itself.
3. **Review change impact.** Compare previous and target upstream revisions, plus
   conflict resolutions. Inspect service code, shared API/subsystem/auth libraries,
   integration/golden tests, schemas, config, charts, nginz routing, Nix/Cabal
   dependencies and operational changes. Inventory transitive impacts, not just
   files below one service directory. Record each candidate as affected, unaffected
   with evidence, or unknown; unknown means affected until resolved.
4. **Invalidate affected claims before advertising the update.** Set each affected
   service's `compatibility` to null and queue bounded contract/candidate/verification
   tasks. Preserve old gate records and the prior pinned versions in git history
   and release/runbook evidence. An unchanged candidate may retain a claim for its
   old exact baseline; this never establishes support for the new upstream SHA.
5. **Verify the default path first.** Use existing Nix/Cabal checks for Haskell
   builds, unit tests, formatting and dependency alignment. Verify that old config
   and ordinary install/upgrade commands keep their meaning, omitted selection
   stays Haskell, and existing Haskell workflows do not require Go. Compare rendered
   deployment defaults to the same upstream version without fork-specific options;
   distinguish intentional upstream changes from accidental fork changes. Follow
   root policy on integration execution; unavailable checks remain blocked.
6. **Revalidate candidates separately.** Pin imported upstream, built Haskell
   reference and built candidate SHAs, image/config/fixture hashes and peer versions.
   Adapt contracts to upstream through separately reviewed contract-change tasks;
   do not suppress upstream behaviour to make Go pass. Run the matrix below in
   permitted environments. Record a new completed verification task and populate
   compatibility only for the exact tested tuple. A shared upstream update may
   proceed with candidates unverified; do not weaken default Haskell checks.
7. **Review and publish accurately.** Review the merge/conflict resolution, remaining
   upstream patch list, Haskell evidence, candidate support matrix and blockers.
   Release notes identify tested upstream/candidate versions and unavailable Go
   options. Production upgrades remain separate operator-approved tasks. If an
   opted-in deployment cannot safely upgrade, retain its pinned release or use the
   tested switchback sequence; never silently select Haskell on its behalf.

## Required verification matrix

| Configuration | Evidence required before claiming support |
| --- | --- |
| Default all-Haskell | Existing build/unit checks, unchanged fork defaults/config semantics, install and upgrade behaviour, no new Go requirement |
| One Go service with Haskell peers | Full contract parity, security/operations limits, routing/discovery and supported API versions |
| Multiple Go services | Declared interacting combinations, queue/event/federation compatibility and failure recovery |
| Haskell to Go and back | Data written by either implementation stays usable, in-flight work/queue ownership handled, health/shutdown and resource behaviour preserved |
| Upstream/version upgrade | Explicit old/new schema and peer-version support, new behaviour and regression coverage, documented upgrade and switchback constraints |

Start with one Go service and Haskell peers. Expand the published support matrix
as candidates become available; do not imply all combinations have been tested.
Security and negative tests must still pass. A review can justify an unaffected
scenario's retained evidence, but cannot relabel an old run as executed on new
revisions. Root policy currently blocks integration execution without explicit
authorization; writing this procedure does not authorize those runs.

## Compatibility records

`compatibility: null` means no current verified Go baseline. Otherwise the service
record pins `upstream_revision`, `reference_revision`, `candidate_revision` and
`verification_task`. The reference and candidate can be built from the same fork
commit, but must be recorded explicitly. The report includes those SHAs and the
fixture/config/image identities; the task's evidence revisions identify the
candidate checkout. Its criteria cover the matrix and justify applicability.

The validator checks record consistency, not whether a remote branch has advanced,
whether a SHA actually contains the claimed upstream changes, or whether a report
proves compatibility. Review the git ancestry, source differences and real artifacts.
Clearing a claim affects support metadata only; it does not change running services.
