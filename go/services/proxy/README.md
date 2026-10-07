# Optional Go proxy candidate

Work in progress under the complete-service
[implementation plan](../../../docs/go-migration/plans/proxy-implementation.md).
Haskell remains the supported default. This runnable candidate implements the
seven provider handlers, health, V0..V19 routing, ordered/opaque queries,
Configurator-style secrets (groups/imports/interpolation), 60-second reload,
trusted TLS, retries and basic signal shutdown. It is **not a verified alternative**.
Complete request-ID/metrics/log compatibility, remaining parser/transport/gzip/
reload/cancellation/shutdown characterization, real differential evidence and
optional selection/switchback remain required. No service availability is claimed.

Pinned Go 1.26.8 and YAML v3.0.4 (YAML organization's security-maintained stable
line). Existing option names, unknown-field acceptance, duplicate-key errors and
ordered/opaque query handling are tested. Returned configuration errors exclude
input values. Tests include expectations from the independently captured Haskell
baseline, not candidate-generated goldens.

Run `python3 go/check.py proxy` from the repository root with the pinned toolchain
on PATH. Passing component/scanner checks do not imply a whole service exists or
that full configuration/transport semantics have been characterized.

Build/run `cmd/proxy` using `-c`/`--config-file`; the default remains
`/etc/wire/proxy/conf/proxy.yaml`. Existing provider endpoint overrides and secret
file mounts are used. `disableTlsForTest` selects plain HTTP in the same test-only
configuration role as Haskell; it never enables `InsecureSkipVerify` for TLS.
Do not connect this development candidate to production while compatibility is
unverified. Independent oracle files belong to a separate task and cannot be
changed to make these candidate tests pass.

Current checks: module tidy/verify, gofmt, vet, unit/race tests, executable build,
module vulnerability scan (none found), and allowed license check passed.
License report identifies the local copied AGPL license and YAML v3's MIT
license; the unpublished module's generated HEAD URL is a warning, not evidence
of a published license URL. The local license files were inspected. These checks
are candidate code evidence, never Haskell unit success or Haskell-versus-Go parity.
