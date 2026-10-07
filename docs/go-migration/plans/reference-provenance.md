# Pinned Haskell reference discovery

Find a defensible source/image relationship for proxy and cargohold at inspected
upstream SHA f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Published proxy 5.37.4 has
immutable image digest but no source label; release tags/dates are insufficient.

Use an isolated task cache, never edit reference code in this checkout. Export
the exact git tree, initialize only the exact publicly accessible generic-message
submodule in that copy, and record archive hashes/gitlink. Do not synthesize an
enterprise stub or replace dependency pins. Try selected-service Nix derivation
evaluation and inspect the closure/expected output before deciding whether a
build fits available resources. Source/image matching and real build receipts
remain unverified until actual evidence is available.

Security/environment: Python standard library, verified HTTPS downloads, fixed
argument-vector commands, no credentials or interactive auth, no shell commands
from task data. Docker nixos/nix@sha256:c7cc6c8cb5d81bed19997247629604708fda95c99c43ac362daa05b6a68e8a24
gets a read-only source mount. It may fetch build dependencies; no Haskell service,
existing integration suite or parity environment is launched. Use synthetic data
and task-owned temporary directories/containers only. Preserve original source
and Nix/Cabal inputs exactly. Root build output filter applies to build commands.

Plan: export/source receipt; public submodule retrieval; Nix evaluation of
`packages.x86_64-linux.wireServer.imagesNoDocs.proxy.drvPath`; inspect outcome,
then repeat for cargohold if evaluation succeeds; record blockers/provenance.
Do not claim source provenance from the evaluated store path alone: it is an
input/output identity to check against a trusted build or published receipt.

Acceptance: record verifiable pins and results or explicit missing inputs/build
constraints, with reproducible commands and no invented reference verification.
Native Haskell tooling and whole-project enterprise-source access are unavailable
until independently provisioned. This discovery advances no service gate.
Rollback: remove task-owned temporary source/container files; root is unchanged.
