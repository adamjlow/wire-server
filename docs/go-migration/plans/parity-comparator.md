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

## Results and remaining evidence

28 comparator tests pass. They independently corrupt status, body, headers,
ordered provider query/effects, request stimulus, step order, identity pins and
assertions. They verify JSON null/missing, bool/number, numeric spelling, array
order, duplicate members and UTF-8 handling; blocked/skipped/missing capture and
unverified provenance cannot pass. CLI tests cover hashes, exclusive reports,
symlinks, oversized/nonregular input and capture-value non-disclosure. A policy
test proves candidate tasks cannot claim the comparator after oracle protection
was added to both service records. All 32 migration validator tests also pass.

No real service was launched. Offline reports explicitly state that supplied
assertions/provenance are declarations and parity is not verified. Human oracle
review remains pending; no lifecycle or compatibility claim is advanced.

Reference investigation confirmed the inspected SHA exists in public upstream,
using its immutable [commit API](https://api.github.com/repos/wireapp/wire-server/commits/f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e).
The registry's [proxy manifest API](https://quay.io/api/v1/repository/wire/proxy/manifest/sha256:460629f6358c93892c3fd6ae5177c685bd787212900339a3bdab2530f875a8f4)
exposes no source label in its metadata, and public release notes do not tie that
image digest to the inspected SHA. This is still unverified reference provenance.
The generic-message-proto submodule's exact gitlink is publicly resolvable; the
enterprise submodule repository returned anonymous HTTP 404. Selected-service
Nix evaluation/build may avoid enterprise; whole-project checks cannot assume
that private source is available. No failure is relabelled as a pass.
