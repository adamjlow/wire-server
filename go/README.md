# Go candidates

Current patched five-service artifacts use **Go1.26.9**. See the
[identified images, compiler receipts and original77/container results](../docs/go-migration/qualifications/go-patched-20261009/README.md).
Historical Go1.26.8 receipts remain history; use the new identified artifacts.
Haskell remains the supported default.

This directory holds optional Go tooling and isolated service implementations.
Haskell remains supported and default. Go binaries are explicit per-service
options; existing Haskell workflows have no Go toolchain requirement.

Start with the [migration guide](../docs/go-migration/README.md) and local
[policy](AGENTS.md). Service directories use `go/services/<service>/`.
Create them only when a contracted service's executable plan needs them.

The optional shell is `nix-shell go/shell.nix`. It pins Go 1.26.9 using official
archive checksums and the existing root nixpkgs lock without changing `flake.nix`
or its default shell. Alternatively use exactly the version in `.go-version`.
Disable automatic toolchain upgrades with `GOTOOLCHAIN=local`.

Run `python3 go/check.py proxy` (or another service) for formatting, dependency
alignment/verification, vet, race/unit tests, build, vulnerability and license
checks. Missing modules fail explicitly. The tool has no parity or deployment
permission. License detection accepts the repository's AGPL-3.0 plus Apache-2.0,
BSD-2-Clause, BSD-3-Clause, ISC and MIT; unlisted/unknown results fail.
License reports still require human assessment of compatibility
with this repository's AGPL license; automated detection alone is insufficient.

The separate `tooling/` module pins the two scanners and their transitive
dependencies. Candidate modules do not depend on it. CI checks each module that
exists and makes no service validation claim when none exists. Tooling boundary
tests run with `python3 -m unittest discover -s go -p 'test_*.py'`.
Run `python3 go/check.py tooling` to check the scanner module itself; CI always
runs this even when there are no candidate modules. This is tooling evidence only.
Every module includes a copy of the repository's `LICENSE`, so license discovery
works within Go's module boundary. Do not exempt the candidate from license checks.
