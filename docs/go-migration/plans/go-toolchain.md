# Optional Go toolchain

Purpose: reproducible Go checks without altering Haskell defaults. Use one module
per candidate at `go/services/<service>`, plus an independent tooling module.
Pin Go 1.26.8 and official archive SHA256 hashes. Nix opt-in shell reads the existing
root nixpkgs lock; it adds no input or package to the default Haskell environment.

Security: use official HTTPS downloads and checksum verification; no shell execution
of task text; reject service path traversal/symlinks; require the exact pinned Go
version with GOTOOLCHAIN=local and module verification. Pin vulnerability/license
tools and their dependency checksums, fail on missing modules or failed checks.
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
