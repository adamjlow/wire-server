# Delegated preparation review and isolated reference characterization

User authority: the explicit 2026-10-07 request to review these ourselves and
self-approve if they meet the migration goals. This follows the concrete
proxy/cargohold contracts and synthetic isolated Docker environment proposal.
Record same-author approval accurately, never claim an independent actor.

Bounded scope: record the delegation, source/tooling/oracle preparation review,
and authorize a dedicated reference characterization runner. Preserve the
prohibition on existing integration suites and production changes. No security
exception, authentication change or equivalent candidate is approved.

Environment: task-owned Docker internal network (no external egress), reference
image pinned by immutable digest, synthetic provider in a scratch container,
trusted generated TLS CA, read-only mounts/root filesystems, no capabilities,
no host network or privileged containers, client endpoints published only to
literal loopback on ephemeral ports. Use temporary task-owned storage; remove
only containers/network created by this runner. Capture bounded synthetic
requests/responses, provider effects, source/image/config/fixture hashes. Never
execute command strings from task records. No live providers, credentials,
shared databases or existing integration executables.

Steps: queue/claim; write durable review decision and narrow policy exception;
run migration state/transition/scope checks and validator tests; commit code;
record exact-revision evidence and delegated same-author review. Existing tasks
may cite this decision, but each must satisfy its own substantive criteria.

Security: Python 3 standard library orchestration, pinned Go 1.26.8 static fake
provider using crypto/tls and crypto/x509, Docker isolation, no new library
choices. Require endpoint/input limits, failure cleanup, synthetic-only logs.
Reference traffic uses trusted CA verification, not insecure TLS.

Rollback: revert only this policy exception if delegation is revoked; keep
published Haskell defaults/reference source intact. No deployment is changed.
Acceptance: bounded authorization and truthful review provenance are recorded;
all migration validation/tests and scoped diff checks pass. This alone supplies
no service implementation, fixture completion, runtime parity or gate.
