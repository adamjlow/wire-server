# Experimental Go candidates

This is an AI-assisted service replacement experiment and a basis for measured
Go-versus-Haskell comparisons. It is not a completed migration or a claim that
Go is faster. See the [experiment scope, remaining services, layout and test/benchmark options](../docs/go-migration/README.md).
Brig is still in progress; Galley has not started. Haskell remains supported
and default, with Go selected explicitly per service.

Current patched artifacts use **Go1.26.9**. The
[five-service qualification](../docs/go-migration/qualifications/go-patched-20261009/README.md)
identifies the unchanged peer images/compiler receipts. The optional
[background-worker](services/background-worker/README.md) now completes the six-Go
mixed stack: [all124 original executions pass](services/background-worker/evidence/final-original124-20261010/)
with remaining Haskell peers, plus finite operations and durable switchback.
The optional [Spar](services/spar/README.md) adds the seventh Go service:
[all225 original executions qualify](services/spar/qualification/evidence/final-original225-batched/result.json)
through six complete suites in three owned batches with identical selected
artifacts and Haskell remainder. Its [review](services/spar/REVIEW.md) records
current trust/protocol, exact-image loaded shutdown and actual durable switchback
proof, inherited concerns and retained failed attempts.
Historical Go1.26.8 receipts remain history; use identified current artifacts.
Haskell remains supported/default.

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
