# Optional Go Brig (implementation in progress)

This module implements optional Brig slices on the existing native schemas.
It is still being built and is not a selectable whole-service replacement.
Haskell remains supported and default.

Implemented slices include account/client reads, password and cookie
authentication, email activation, internal registration (including SAML and team
invitation acceptance), and their supporting storage, index and queue writes.
Registration mail template/MIME rendering is implemented separately from delivery;
The SES delivery adapter passes signed HTTP controls. SMTP, startup selection
and public `/register` wiring are still pending.
The process also serves liveness/version responses, applies native PostgreSQL
migrations and drains HTTP on TERM. Cassandra schemas must be initialized with
the existing native operator utilities. Go uses the repository's pinned patched
toolchain and service module; there is no Haskell runtime embedding or dependency
fork.

Evidence records the exact source and artifact tested. Selected executable
native→Go→native witnesses cover [email activation](qualification/evidence/activation-switchback/README.md),
[internal identityless registration](qualification/evidence/registration-switchback/README.md)
and [SAML rejoin](qualification/evidence/client-bulk-saml-switchback/README.md).
[Invitation acceptance](qualification/evidence/registration-invitation-core/README.md)
currently has source, unit and actual native-schema storage evidence;
its original end-to-end/API witness remains outstanding. Earlier passing
witnesses do not automatically qualify a newer binary or the complete service.

Public registration policy, mail sequencing and session composition have
[unit and compiler evidence](qualification/evidence/registration-public-core/README.md).
Native mail adapters and the live public HTTP endpoint are still pending.

Remaining work includes public registration/mail delivery and invitation creation,
SCIM provisioning, client writes/prekey/MLS/DPoP operations, connections,
properties, OAuth/app/provider/federation families, complete configuration/docs,
queue consumption/discovery and final service/image/selector qualification.
Unimplemented business paths have no successful fallback response.
The migration utility's fresh-schema/ledger/checksum/advisory-lock observations
are separately recorded; their scope does not establish the business APIs.

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
