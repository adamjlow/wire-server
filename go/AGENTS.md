# Go candidate policy

Root policy applies. Each candidate is an opt-in alternative for a whole service
through existing network, queue and storage contracts; never embed Haskell or cross the runtime
boundary via FFI. Do not port Polysemy, lenses or Haskell module boundaries by rote.
Keep Haskell supported and default. Isolate Go sources and tooling, minimize edits
to upstream files, and keep both implementations compatible with upstream schemas.
Do not require Go tooling for an existing Haskell build or development workflow.

Use idiomatic Go: explicit dependencies and errors, small interfaces at real
boundaries, context cancellation and deadlines, bounded concurrency and resource
use. Preserve observable authentication, authorization, error, serialization,
retry, ordering and shutdown semantics. Do not introduce a shared Go framework
until multiple services demonstrate a concrete need in a reviewed plan.

Before the first implementation, a scoped toolchain task must pin a Go version
in optional Nix outputs/dev shells and Go CI, choose module boundaries, and add
explicit build/test commands without changing default Haskell targets.
Prefer one module per service unless a documented decision justifies otherwise.
Service modules and a separate tooling module now exist. Run checks for one
module with `python3 go/check.py <service>` from the repository root; there is
no combined root Go module for `go test ./...` from this directory.
Each eventual module must run gofmt, go vet, unit tests and race tests, dependency
vulnerability/license checks and its required compatibility tests. Commands and
versions belong in its plan and evidence. Preserve the Haskell build alongside it.
Support claims must name tested upstream/reference/candidate revisions and mixed
deployment combinations. Follow [upstream sync](../docs/go-migration/UPSTREAM.md)
on reference changes; old passing evidence does not establish current compatibility.

Candidate unit tests may change with implementation. Shared parity assertions,
fixtures, reference code and normalization rules are protected oracle material;
change them through separate contract tasks. Do not disable failures, loosen
comparisons or hide mismatches as part of candidate work.
