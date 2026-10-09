# Architecture and boundaries

The current source of truth is `services/<name>/`, with behaviour also in
`libs/wire-subsystems/`, API definitions in `libs/wire-api/` and federation types
in `libs/wire-api-federation/`. Trace those dependencies in inventories; directory
placement alone does not define a service's complete contract.

Candidates live in `go/services/<name>/` as independent executables/images.
They communicate using existing HTTP, federation, WebSocket and queue protocols,
and must preserve relevant storage formats and semantics. A Go service can talk
to Haskell peers indefinitely. No shared runtime or per-Haskell-module migration.

Deployment selection chooses the Haskell or Go image independently per service
behind its existing identity, ports, routes and discovery. Omitted selection must
preserve Haskell and the existing config defaults. Existing values files, image
overrides and install/upgrade commands must retain their meanings. Reject invalid
or unsupported choices clearly; never silently switch implementations. Existing
per-service Helm postrenderers change exactly the selected container image and
leave omitted selection byte-for-byte unchanged. Actual selection proofs and
local image receipts are linked from the
[current qualification](qualifications/go-patched-20261009/README.md).

Changes to Helm/Nix, nginz routing and config get scoped operations tasks. Keep
Haskell sources, builds, images and configuration supported indefinitely; Go
availability does not retire them. Pin both selected images and tested revisions.
Publish supported mixed-implementation/version combinations and switchback limits.
Default all-Haskell deployments must not build Go or require new settings.

Do not mirror production writes to both implementations. Parity runs use isolated,
equivalently seeded stores and fake external systems; authorized read-only shadows
need an explicit data-handling plan. Queue consumers require exclusive ownership
or separate queues. Storage migrations, new protocols and architecture redesigns
are separate decisions with their own rollout/rollback plans.
Go must use compatible upstream schemas and persisted formats; do not add Go-only
schema migrations as a prerequisite for choosing an implementation. Demonstrate
Haskell reading data written by Go after switchback, not just an image rollback.
Upstream's own schema upgrades retain upstream's documented version constraints.

The existing [optional toolchain](../../go/README.md) pins Go versions, module
layout and explicit checks. New service plans must cover image build,
health/readiness and graceful shutdown. Expose Go through optional
Nix outputs/dev shells and explicit targets while retaining existing defaults.
The optional tooling keeps the existing Haskell build independent of Go.
Shared Go libraries need actual reuse and a serialized owner; avoid building a
speculative replacement for `libs/`.

Keep implementation files in `go/`; keep edits to upstream services, shared APIs,
builds and charts narrowly scoped. Do not copy chart trees or rename upstream
directories to make room for Go. Track unavoidable edits and their justification
in the service plan; revisit them on every [upstream sync](UPSTREAM.md).

Beyond the initial nine services, this tree also contains nginz, stern and an
enterprise submodule. They are not silently included in migration scope. Record
their interactions; register new migration units only with a documented boundary.
