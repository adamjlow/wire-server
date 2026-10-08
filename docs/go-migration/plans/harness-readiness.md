# Automated migration harness readiness

User authority: 2026-10-08 request to trust AI implementation/review, use appropriate
surface-area tests, establish automated setup first and reuse existing e2e tests.
No manual testing step or routine reapproval is required. Preserve real evidence,
security boundaries, Haskell defaults and human control of production switches.

Deliver one-command existing proxy differential execution plus an additive
upstream e2e binary-selection adapter, an explicit required-test catalogue and
fail-closed report checks. Original Haskell tests/assertions remain unchanged.
Reuse integration's services-directory/dist resolution through an ephemeral
layout; do not replace binaries in the real checkout. Full e2e execution needs
its actual provisioned disposable dependency environment and Haskell binaries;
missing tools/environment/tests are blocked, never pass. This task does not
claim e2e execution where that environment is absent.

Security: Python stdlib and existing pinned Go/Docker tooling, fixed argument
vectors, exclusive external artifact directories, non-symlink executable inputs,
explicit allowlisted service/test selection, no shell evaluation, no production
targets/secrets, no fixture weakening, cleanup restricted to owned paths. Existing
synthetic proxy runner supplies verified TLS and internal networking. The e2e
adapter prepares and validates selection; executing the original suite remains
subject to a provisioned authorized disposable environment, never ordinary host
services. Do not add an unverified general remote execution mechanism.

Acceptance: original e2e request/assertion source is untouched; staged selection
preserves Haskell defaults and peers; unknown services, missing tools, empty or
incomplete/failed/skipped test reports cannot pass; one-command proxy API run
produces real capture and comparison. CI runs harness negatives and retained
proxy comparisons. Document remaining e2e environment work truthfully.

Stop rule: per service, cover its public routes and side effects with upstream
assertions, add only gaps for auth/ownership/version/error/storage/retry/rollback
or identified regressions, perform one bounded operational smoke and switchback.
Native runtime diagnostic values are reviewed as diagnostic measurements, not
customer API equality; genuine security/behavior changes still need explicit
contract disposition. No exhaustive parser/performance permutations or repeated
builds for metadata-only changes. Changed Haskell packages get Haskell checks;
additive Go work preserves Haskell CI without waiting for an unrelated full rebuild.

Checks: Python harness/comparator negatives, real proxy API runner, retained API
and operational comparison, migration checker/32 tests, scoped diff/whitespace.
Keep all commits local on go-migration. No deployment or cargohold migration here.
