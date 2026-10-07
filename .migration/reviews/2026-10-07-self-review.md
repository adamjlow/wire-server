# User-delegated preparation review, 2026-10-07

Authority: the user requested, "Please review these yourself, to they meet our
goals and technicall assure that what we refactor offers the same contract? if so
self approve." The reviewed material includes the proposed proxy/cargohold
contracts, preparation tooling and concrete isolated parity environment.

Reviewer: Codex, the same actor/owner as the preparation author. A task review
label `codex (self-review delegated by user)` expressly denotes this same actor;
it is not an independent reviewer identity or human technical review. The
validator checks strings and evidence structure, not reviewer identity or
authorization. No claim is made otherwise. This instruction supersedes the
generic independent-review requirement for the bounded preparation listed here.

## Decisions and scope

- `delegated-review`: approve this accurately scoped policy exception and the
  synthetic isolated reference environment. Original integration executables,
  shared infrastructure, production switching and security exceptions are excluded.
- `proxy-inventory`: approve the source-cited boundary discovery at
  f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Methods/query edge cases are made explicit
  in the proxy contract; unknown runtime details remain characterization work.
- `cargohold-inventory`: approve the corrected inventory at 20853a7dd52ea73b1e68dbc8760cb12c8e08dd6e,
  including the source's discarded federation domain and legacy OTR owner check.
- `validator-fixtures`: approve synthetic fixture isolation, unchanged negative
  validation assertions and 32 passing validator tests. This is not service evidence.
- `go-toolchain`: approve optional pinned tooling, argument-vector commands,
  strict source/module paths, fail propagation, race/vet/vulnerability/license
  checks and additive CI. Eight boundary tests and documented native/Nix checks
  establish preparation only. Scanner limits and manual license/native-code
  assessment remain explicit; candidate dependencies require their own checks.
- `parity-comparator`: approve the offline comparison foundation reviewed in
  proxy's contract at 20853a7dd52ea73b1e68dbc8760cb12c8e08dd6e. Its 28 positive/negative
  tests cover bounded inputs, identities, coverage and meaningful mismatch
  preservation; supplied captures always produce `parity_verified: false`.
  It supplies no live capture/provenance, gzip/framing decision or cargohold
  generated-value/signature mapping. Approve only the foundation criterion.
- `reference-provenance`: approve discovery and use of the two immutable trusted
  publisher images as the explicit characterization baselines, given the
  documented exact source-evaluated Entrypoint/Env/User match. This does not claim
  an independent binary rebuild, signed build attestation, Haskell unit success
  or candidate parity. Reports must identify this provenance method honestly.
- `proxy-contract`: approve source-derived requirements, not task completion
  before executable baseline fixtures exist. Additional fixture/normalizer work
  must receive explicit source-based review before a candidate consumes it.
- `cargohold-contract`: approve full-service/storage/switchback scope but withhold
  full contract approval pending the security/source conflict and baseline fixtures.
  No changed or bypassed authentication behaviour is authorized.

The review does not advance verified/available stages, populate compatibility,
approve an unimplemented candidate, or waive any unrun or failing substantive
check. Root Haskell source/build/config/defaults stay unchanged. Preparation
tasks may finish only when their particular criteria really pass; reviewer
annotation alone proves none of them.

## Authorized characterization environment

Use only task-owned containers and an internal Docker network with no external
egress, generated synthetic provider credentials/data, verified TLS with a
generated trusted CA, immutable reference identities, read-only mounts/rootfs,
dropped capabilities and no privileged/host-network containers. Bind any client
ports only to literal loopback using ephemeral ports. Fixed argument vectors,
bounded captures and timeouts, exclusive artifact directories and guaranteed
cleanup are required. Do not touch existing containers or shared databases.
The runner must validate its own fake-provider behavior with unit tests and
record actual observed Haskell results; mocks alone cannot be parity evidence.

Existing integration suites and production remain prohibited. Performance and
operational thresholds are still required before verification/load testing.
