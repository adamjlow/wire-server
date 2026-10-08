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
