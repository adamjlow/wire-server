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
