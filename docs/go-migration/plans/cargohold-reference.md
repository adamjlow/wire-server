# Cargohold reference characterization

The preceding goal turn completed Docker harness qualification: Haskell proxy
5/5, cargohold core 27/27, and selected Go proxy 5/5 pass. This task advances the
Go cargohold goal by resolving the specific source findings blocking its contract.
Do not implement Go or change the reserved cargohold contract/oracle here.

Source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; trusted published Haskell
runtime from qualified runner image fc3e23a19bfa10c7db2a73ade0bfc1fad87aa391514577bdc8d4d90843fcbbc8
(build c75686850f4d92c13a41e311b86bc861e8b86271). Source attestation limits
remain as documented. Use a separate private Docker network, non-root read-only
runner/store, no host ports or external egress, synthetic fixed UUIDs/tokens/data,
explicit immutable images, bounded CPU/memory/time, fixed argv and own cleanup.
S3 speaks the upstream local-test HTTP protocol inside this private network;
never disable TLS verification or send fixture credentials outside it.

Seed exact source-derived v3 persistent user/token metadata and legacy OTR
user metadata directly in the task-owned S3 store. Observe get-asset/stream-asset
for foreign-domain matching/different UUIDs with missing/wrong/correct tokens;
legacy OTR same/different requesters plus missing/malformed stored user. Download
any signed fixture URL and verify synthetic bytes; retain sanitized URL structure,
not raw signatures. These are service-layer observations, not a claim of ingress
exploitability. Legacy encrypted shared-asset read semantics need context before
calling the ignored uploader identity an authorization defect.

Also characterize the finite parser gaps named in the source review: unequal
multipart delimiters, unused MD5, decimal length suffix, incomplete declared data
and persisted effects. Use the source-defined bot upload endpoint to exercise the shared parser without
Brig eligibility dependencies; ordinary user eligibility remains covered by the
qualified original e2e suite. Do not translate original integration assertions.
Keep exact baseline observations; stricter behavior must not be called parity.

Commands: `python3 tools/parity/cargohold/reference.py --output /absolute/new-dir`;
focused stdlib unit checks and `make migration-check migration-test`; scoped
validator against the claim commit. Retain reference JSON with actual identities
and S3 effects. Review observations honestly under existing user self-review
and isolated characterization authority. If behavior needs human contract/security
disposition, present a concrete observed choice; progress on other safe preparation
while awaiting it. No production change, push or candidate equivalence claim.

## Actual reference outcome

20 real reference cases completed at 14d93fd039ef56e1a309f7668623c59690411afb
using the qualified immutable runner and real task-owned Garage S3. Retained
[observations](../../../tools/parity/cargohold/reference.json) and
[identities](../../../tools/parity/cargohold/run.json) are service-layer source
characterization, with `candidate: null` and `parity_verified: false`. The actual
cargohold binary hash matches the earlier compiled-suite preflight exactly.

- Foreign origin plus matching owner UUID: get-asset available true and stream
  returns the seeded private bytes, with missing or wrong token. Different UUID
  plus missing/wrong token: unavailable/404; correct token: available/real bytes.
- Legacy OTR: same and different requester obtain 302 and valid signed downloads
  of the seeded bytes. Missing/malformed stored user returns 404. This records
  existing behavior; the uploader identity is not an owner-only read rule.
- Shared bot/user/provider multipart parser: canonical, unequal delimiters, wrong
  MD5, decimal length suffix and missing trailer each return 201 and store the
  exact 13-byte payload as octet-stream with bot/token metadata. Incomplete data
  returns 400/incomplete-body but leaves a completed 10-byte S3 object; no open
  multipart upload remains. Do not silently change these persisted effects.

The first attempt lacked the required bot token-type header and observed 403
before parsing. A subsequent tool correction distinguished relative 201 upload
Locations from signed 302 downloads. Signature verification needed the exact
signed Host header, not the loopback IP default. Final controls verify real
bytes and fail if authorization/status/source expectations differ; earlier failed
probes are not reported as passing captures. Four focused tests enforce fixture
isolation, signature redaction/Host fidelity, relative Locations and short-body
construction. They are discovered by the existing parity CI unit command.

User decision requested after these concrete observations: preserve existing
federation UUID behavior for exact migration parity and track a security fix
separately, or fix the check in both runtimes first. GUARDRAILS.md reserves unsafe
reference disposition to the user; self-review cannot invent that exception.
No Go code or full contract approval is claimed while this decision is pending.

Same-author review: **codex (self-review delegated by user)** under the existing
isolated characterization authority. Approve the source observation outcome,
not a changed/bypassed authentication contract or candidate equivalence. All
owned capture containers and private networks were removed. Original Haskell
source/defaults/fixtures and original e2e assertions remain unchanged.
