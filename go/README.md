# Go candidates

This directory reserves the home of future Go service implementations. There is
no Go implementation, module, selected toolchain or deployment change yet.
Haskell remains supported and default. Future Go binaries are explicit per-service
options; existing Haskell workflows must not acquire a Go toolchain requirement.

Start with the [migration guide](../docs/go-migration/README.md) and local
[policy](AGENTS.md). Future service directories use `go/services/<service>/`.
Create them only when a contracted service's executable plan needs them.
