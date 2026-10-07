# Optional Go toolchain

Purpose: reproducible Go checks without altering Haskell defaults. Use one module
per candidate at `go/services/<service>`, plus an independent tooling module.
Pin Go 1.26.8 and official archive SHA256 hashes. Nix opt-in shell reads the existing
root nixpkgs lock; it adds no input or package to the default Haskell environment.

Security: use official HTTPS downloads and checksum verification; no shell execution
of task text; reject service path traversal/symlinks; require the exact pinned Go
version with GOTOOLCHAIN=local and module verification. Pin vulnerability/license
tools and their dependency checksums, fail on missing modules or failed checks.
License detection explicitly accepts the repository's AGPL-3.0 and common
permissive Apache-2.0/BSD-2-Clause/BSD-3-Clause/ISC/MIT identifiers. Generic license
categories classify this repository's own AGPL as forbidden; therefore use exact
identifiers, reject unknown/unlisted results and retain human compatibility review.
CI has read-only contents permission, no persistent git credentials and a timeout.
Go tooling must not execute or contact production services. TLS remains verified.

Steps: add shared version/archive pins and standalone Nix shell; add tooling
module with govulncheck and go-licenses; add explicit Python check command and
optional Go CI; exercise fail-closed boundaries with unit tests, then test native
Go and Nix shell where available. Record unsupported/unavailable checks honestly.

Commands: `python3 -m unittest discover -s go -p 'test_*.py'`;
`python3 go/check.py proxy` (must fail until a module exists);
`nix-shell go/shell.nix --run 'go version'`;
`make migration-check migration-test`; `git diff --check`.
No candidate parity, default changes, or lifecycle advancement occurs here.

Rollback: remove only these additive tooling files/workflow; Haskell requires none.

Evidence and limitations will be recorded below at the tested commit.

## Verification results

- Eight boundary tests pass: missing modules, unknown/traversal/shell payloads,
  symlinked modules/sources, empty sources, wrong compiler and tool failure.
- Native `python3 go/check.py tooling` passes formatting, tidy/verify, vet, race
  compilation, both scanner builds, module vulnerability scan (none found),
  explicit license identifiers and license report. The tooling package has no
  unit tests itself; this is not service-test evidence.
- A temporary synthetic executable module exercises the complete candidate
  command; missing module/license cases were rejected. No candidate service or
  Haskell-vs-Go parity is claimed.
- Docker `nixos/nix@sha256:c7cc6c8cb5d81bed19997247629604708fda95c99c43ac362daa05b6a68e8a24`
  with a read-only repository mount successfully loaded the optional Nix shell:
  `go version go1.26.8 linux/amd64`. The same environment ran nixpkgs-fmt on
  `go/shell.nix`; its formatted output is committed. Other platform archives
  have official verified checksum pins but their shells remain untested.
- License reporting warns that assembly may contain dependencies it cannot
  inspect and that this unpublished module's generated HEAD URL may be wrong.
  Inspect local licenses at the tested commit; manual compatibility/native code
  assessment is still required. No warning is suppressed.
- Existing Haskell source, libraries, integration suite, root flake/lock, Nix
  sources and Cabal project are unchanged from the inspected reference. Native
  Haskell builds/unit tests remain unavailable; no integration suite was run.
- Migration state and 32 validator tests pass; diff/scope checks are required
  again at handoff. All work remains local; remote CI has not run.

The toolchain and CI are ready for independent review. These results do not
advance either service's stage or fill any compatibility claim.
