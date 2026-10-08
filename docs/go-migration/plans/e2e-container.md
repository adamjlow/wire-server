# Containerized upstream e2e runner

User requested a Docker e2e setup on 2026-10-08 to keep Haskell tooling and backend
dependencies off the host OS. Build an additive non-root runner from Wire's pinned
published integration image, reuse original tests and the existing binary-selection
adapter, and prove executable/test discovery inside a restricted container. Host
requirements stay Docker and the existing lightweight orchestration only.

Reference image: quay.io/wire/integration@sha256:f5db439022d1c0ccf071c25f9c0d132ed21a9e5e3647db59c593d33614bc4f27
(resolved from published 5.37.5; same release tags as the pinned proxy reference).
This is trusted publisher provenance, not independent binary rebuild/source
attestation. Discover and record actual binary/image identities. Do not claim
original e2e test success from listing/preparation. Backend dependencies belong
in task-owned Docker services on private networks, not installed on the host;
full runtime qualification remains dependent on that stack.

Security: immutable base digest, no production data or secrets, fixed commands,
read-only checkout/rootfs, writable dedicated tmpfs/output only, non-root UID,
dropped capabilities/no-new-privileges, no Docker socket/host networking/published
ports, bounded resources, own container/network cleanup only. Read-only listing
inside this container is authorized preparation under the user request; do not
launch existing full integration tests on ordinary/shared host dependencies.

Acceptance: actual runner image builds, original integration and service binary
preflight works, original proxy/cargohold test names can be discovered, selection
keeps Haskell peers and requires complete reports, default Haskell inputs unchanged.
Meaningful orchestration negatives, migration/scope/whitespace checks. Document
missing dependency qualification honestly. No service migration/production change.

## Outcome, 2026-10-08

Actual runner image built successfully at
`abc423639c7f7d5bcd0e136a1d88be072ec7738f`, image
`sha256:3fc7271a686bb92bebb0666231d6b6b52fe801ed9c6bc00c3589231668734628`.
[Build receipt](../../../tools/parity/docker/build-receipt.json) retains immutable
base/image/source and input-file hashes.
[Actual preflight](../../../tools/parity/docker/preflight-receipt.json) records
UID 65534, integration/service executable hashes and all five original proxy plus
27 core cargohold test functions discovered from the compiled suite. It ran with
networking disabled, read-only rootfs, capability drops and bounded resources;
only its actual created container ID was removed. No host Haskell toolchain or
backend package was installed. The base bundle is approximately 3.9 GB unpacked;
tiny runner layers reuse that image rather than duplicating its Nix runtime.

42 parity/container boundary tests pass (six new container checks), including
receipt/destination rejection, non-root image enforcement before execution,
compiled-test coverage checking, missing-environment rejection and rejecting an
empty XML report from a zero-exit process. Migration validation/tests (32), scope
and whitespace pass; original Haskell code/fixtures/charts/Nix/Cabal inputs are
unchanged. Runner documentation names exact build/preflight commands and per-service
substitution, without suggesting manual user validation.

The publisher bundle contains the original integration executable and Haskell
service binaries but not a standalone `run-services` executable. The container
uses the existing suite's PATH-launch mode with configs outside `services/`,
mounting synthetic service configs under `/etc/wire`; static Haskell peers must
be provided by the disposable environment orchestrator. Full-suite mode validates
process exit and all required XML cases, and records a separate run receipt.
It is not executed by preflight and is not qualified by mock boundary tests.

Remaining environment work: task-owned backend/peer containers, private networks,
fixture/config initialization and one Haskell baseline run, followed by candidate
selection. The upstream dev Compose file cannot be launched unchanged because it
has fixed global container names/subnets and host port bindings. No full e2e test
success, storage switchback, source attestation or production deployment is claimed.

Review: **codex (self-review delegated by user)** under the user's Docker setup
request and existing explicit self-review delegation. Approve the bounded image
build/discovery outcome and absence of host toolchain installs. This does not
approve unprovisioned e2e execution or advance any service compatibility gate.
