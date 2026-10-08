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

## Outcome, 2026-10-08

The single automated proxy command executed successfully: 103 real API cases
passed, zero failed. Full operational evidence is reused for unchanged service
code rather than spending another two reload intervals on a harness-only change.
Actual local build/capture identities are below; the previous full capture keeps
its original revision and is not relabeled. Raw artifacts remain in the explicit
local cache path; CI is wired to archive its JSON artifacts for 30 days.

```json
{
  "harness_execution_revision": "0fae335b5cb5df7b7794bde68aea8177e327d3ef",
  "candidate_revision": "0fae335b5cb5df7b7794bde68aea8177e327d3ef",
  "candidate_image": "sha256:50967ae927f560a0895080cd8e11b009f61bf84f190bed8efbe20808e3517784",
  "candidate_binary_sha256": "7868aa4b6b914daeb65a8033506dc03963af4ba34b37f0ef0bf4e2b2e7001238",
  "reference_source": "f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e",
  "reference_image": "quay.io/wire/proxy@sha256:460629f6358c93892c3fd6ae5177c685bd787212900339a3bdab2530f875a8f4",
  "result": {
    "schema_version": 1,
    "result": "pass",
    "service": "proxy",
    "profile": "api",
    "coverage": "103 live API cases; operations were not rerun",
    "parity_verified": false,
    "capture_sha256": "73cbcb216d8f8e18a31ae3bb453575392125a589ffd7293c8606bc8fa3704603"
  },
  "comparison_passed": 103,
  "comparison_failed": 0,
  "raw_artifacts": "/home/operator/.cache/wire-migration/harness-proxy-20261008",
  "existing_full_operations_evidence": "docs/go-migration/services/proxy/oracle/operations-final.json"
}
```

36 parity/harness unit tests pass (28 existing comparison-foundation tests and
eight meaningful harness boundary tests), plus 15 retained proxy oracle negatives
and 32 migration tests. Workflow YAML and its embedded selection script parsed
successfully. Git diff confirms Haskell services/libraries/integration, charts
and original Nix/Cabal inputs are unchanged. Remote GitHub CI has not run: commits
are local and have not been pushed. The final helper change affects CI selection
only; the live build/capture/compare procedure exercised above is unchanged.

Original e2e reuse: five proxy and 27 core cargohold test functions are catalogued
from upstream source. All assertions inside each function are retained. Staged
selection tests verify real service working directories, exactly one changed
binary, Haskell peers/config/source unchanged, original argument vectors and
explicit `e2e_executed: false`. Required XML coverage rejects missing, extra,
duplicate, failed, skipped and inconsistent-count results. XML checks alone
cannot authenticate process execution and cannot establish differential effects.

The actual original-suite preflight is blocked: this checkout has no `dist/`
Haskell bundle (`FileNotFoundError`), and its Cassandra/RabbitMQ/S3/federation
dependency environment is absent. No original e2e run, Haskell build success,
cargohold storage adapter, or full harness qualification for other services is
claimed. Provision the disposable upstream environment and execute its Haskell
baseline before migrating cargohold. This is agent/CI work, not a manual user
testing or routine approval requirement. Production and real contract/security
exceptions remain outside ordinary delegated test sign-off.

Self-review: **codex (self-review delegated by user)** under the earlier explicit
self-review delegation and the 2026-10-08 instruction to trust AI implementation
and appropriately bounded validation. Approve this bounded automation/preparation
outcome; do not approve unavailable original e2e execution or service gates.
