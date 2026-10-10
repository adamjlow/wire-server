# Optional Go Brig (implementation in progress)

This module is an initial runnable slice on existing native-initialized schemas.
It parses the existing configuration, loads ZAuth keys, checks Cassandra schema94
and opens PostgreSQL, applies the original PostgreSQL migrations, serves
liveness/version responses and drains HTTP on TERM.
Its source-derived ZAuth codec signs/verifies canonical parsed token fields using
Go's standard Ed25519 implementation. Dependencies use the same pinned versions
as the existing migrated services; no shared Go framework or dependency fork.

This is not a selectable or qualified whole-service replacement. Business API
families, served documentation, remaining dependencies, queue
listeners/discovery/cleanup, storage modes and authentication flows are still
required. Unimplemented business paths have no successful fallback response.
The `migrate-postgres` command supports the native database override and explicit
reset operation. Actual Docker observations of native and Go executables confirm
fresh-schema equality, ledger interchange, checksum rejection and all three
nontransactional advisory locks. The current startup slice requires Cassandra
schemas initialized by existing native operator utilities.

Use the repository's pinned Go1.26.9. From this directory:

```
go test -race ./...
go build ./cmd/brig
```

Complete module checks: `python3 go/check.py brig` from the repository root.
Those checks alone do not prove native parity. The frozen whole-service contract,
535 original eight-Go/Haskell executions, focused controls, operational smoke,
actual same-state native-Go-native switchback and optional image/selector remain
required. Haskell remains supported and default. Never use this unfinished
candidate in a production deployment.
