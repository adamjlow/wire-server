# Independent parity comparison foundation

Prepare an offline comparator for independently collected reference/candidate
traces. This is the first part of the runner, not a substitute for real capture,
Haskell image/source provenance, operational coverage or service migrations.
No candidate consumes this oracle until separate authorized human review.

Scope: tools/parity comparator and unit tests, explicit CI self-tests, protect
tools/parity through both service records, and this plan/task/changelog. Do not
modify reference code, service contracts, candidate code or other guardrails.

Security: Python 3.10+ standard library only; no networking, shell/task command
execution or production data. Inputs must declare synthetic data and loopback
endpoints. Strict schemas reject unknown/duplicate fields, non-finite JSON,
malformed hashes, duplicate/missing scenarios/steps/assertions and inconsistent
identities. Bound trace files/payloads; output only mismatch categories and hashes,
never body/header values or credentials. Create reports exclusively, reject
symlink inputs and never overwrite an existing artifact.

Comparison: HTTP status, all headers (case-insensitive names with ordered repeated
values), exact bytes, optional JSON object member ordering only, and ordered typed
side effects. No header/body exclusions or generated ID/timestamp masking yet.
JSON missing/null, bool/number, numeric representation, array order and duplicate
object members remain meaningful. Real cargo key/token/signature normalization
needs separately reviewed validation/capture rules before availability.

Manifest pins source/image/config/contract/fixture/dependency identities, seed and
clock, environment, required scenarios/steps/assertions and exact body comparison
mode. Each trace must match the corresponding identity. Unverified provenance,
missing capture, skipped/blocked/failed cases cannot yield a successful comparison.
An offline report can establish trace agreement only; it always says it is not
verified compatibility. Assertions/provenance in supplied files are declarations,
not proof that a real Haskell/Go process ran.

Steps: create bounded schemas/comparison/report CLI; negative tests including
corrupted status/body/headers/effects and incomplete captures; add self-test CI;
record checks at a commit and submit oracle for human review.
Commands: `python3 -m unittest discover -s tools/parity -p 'test_*.py' -v`;
`make migration-check migration-test`; `git diff --check`; scoped transition check.
Rollback: remove additive comparator/CI task work; retain historical oracle
protections unless a maintainer authorizes changes. Existing Haskell stays default.

Runtime parity, S3 switchback, production selection, real Haskell build checks and
contract approval remain outside these self-tests and cannot be called passing.
