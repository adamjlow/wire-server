# Optional Go Brig (implementation in progress)

This module implements optional Brig slices on the existing native schemas.
It is still being built and is not a selectable whole-service replacement.
Haskell remains supported and default.

Implemented slices include account/client reads, password and cookie
authentication, password reset/change, email activation, internal registration (including SAML and team
invitation acceptance), and their supporting storage, index and queue writes.
Registration mail template/MIME rendering is implemented separately from delivery;
SES/SMTP delivery, selection and public `/register` wiring pass focused HTTP,
verified TLS, socket and cookie controls. Selected native public-registration
credential comparisons pass. SES feedback consumption and internal blacklist
routes pass a native comparison and an original compiled migration test.
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
SES/SMTP adapters and the live public HTTP endpoint also have focused evidence
and a [native credential switchback](qualification/evidence/registration-public-switchback/README.md).
Complete registration flows and native delivered-mail comparison remain pending.

Remaining work includes wider public registration flows and invitation creation,
SCIM provisioning, client writes/prekey/MLS/DPoP operations, connections,
properties, OAuth/app/provider/federation families, complete configuration/docs,
internal event queue consumption/discovery and final service/image/selector qualification.
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


Public signup credential switchback at source a261c531c (candidate dfe202c081ed7a0ab3037fea7bbaa306b15554b1c4c038930cc5981f3e6d9b99) now passes: fresh Go labeled signup/activation, native consumption of profile/cookies and native acceptance of Go cookie/password on unchanged stores. Full Go checks/static original-runner units, original17 paired observations/four raw IP controls, all13 clean exits and independent cleanup PASS. See [retained evidence](qualification/evidence/registration-public-switchback/README.md). This supplemental observation does not execute original535 or qualify full registration/queue/mail delivery contracts; one asynchronous notification failure is retained with cause/delivery unproved. Brig remains implementing/unavailable; Galley follows whole Brig completion.


Password reset now has actual native→Go→native code/password/cookie interoperability at observeree047a9b5, and real native-schema reset/KDF/cookie composition in all three storage modes at5de8f0e54. Production artifact998d2267b29951d12adb3ba4f4ae19360f0b73e53ea0fead79d5fe23dd34b265 remains byte-identical to full qualified7ca1dfbfb; original17 comparisons plus8 native reset controls match, all13 managed processes exit0 and cleanup audited empty. See [API switchback](qualification/evidence/password-reset-switchback/README.md) and [actual stores](qualification/evidence/password-reset-stores/README.md). These supplemental checks do not execute original535 or modern upstream suites and do not establish whole-service availability. Private begin201, attempts/expiry, SAML/current-password/rate policy and native password→code-delete→cookie-delete order retained; precise pending scope and inherited concerns recorded.

Password existence/change passes [native API and credential switchback](qualification/evidence/password-change-switchback/README.md) at source2764b7493/service250ff5580bac410a56253e472bed0000feda002bd8b460405cf1a0fe73059203. Original17 comparisons plus seven native controls match; native accepts first/replacement passwords written by Go and rejects the revoked original cookie. Full Go/static original-runner checks and all13 clean process exits pass. [Actual three-store checks](qualification/evidence/password-change-stores/README.md) passed at0dacd20cc before the final HTTP media-only fix; their older source scope is explicit. Original535 and modern Haskell password-change execution and final whole-service gates remain pending.

[SES feedback, blacklist and federation bootstrap evidence](qualification/evidence/ses-feedback-switchback/README.md) at sourcea6a5e2d6b/serviceb1b2c453013800695fd5b8670f9fd4dbeee39b7267b676f176969b1a370d19b6 includes matching native queue/admission/API responses and native consumption of Go-written blocklist state. The unchanged original compiled `Migration.BlockList.testBlockListMigration` passes actual Cassandra → migration → PostgreSQL backfill/read/delete with the exact Go Brig artifact. Full Go/static original-runner checks, original17 comparisons, eight blacklist controls, configured federation read/three version responses, all25 clean managed exits and independent cleanup pass. This is one original case with Go Brig/Gundeck and native peers, not final original535/eight-Go acceptance. Legacy SES-specific test is source-only; malformed and populated federation/management contracts remain pending. Properties now have the scoped original-test proof below. Galley begins only after whole Brig completion.


[Property API and native switchback](qualification/evidence/properties-switchback/README.md) passes at source624046eb5/servicef7d0830eb8a32289103f4f3d84f85a05c1b5c5b85f463d4992d2fcabb7dde392: all six unchanged original Haskell `Test.Property` cases, original17 comparisons, 16 exact supplemental native controls, actual native→Go→native raw value/parsed-map reads, full Go/static original-runner checks and all25 clean exits/independent cleanup. Native Cassandra property table, configured limits/count16, first-duplicate parsed objects, raw GET bytes and direct self-event connection metadata are implemented; actual event-delivery/failure and account-deletion cleanup/wider parser edge coverage remain pending. This scoped Go Brig/Gundeck plus native-peer proof does not qualify whole Brig or final535/eight-Go deployment. Client updates now have the scoped native proof below. Galley follows whole Brig completion.


[Client update and native prekey consumption](qualification/evidence/client-update-switchback/README.md) at source85bb0c30b/service34ff1c754b7bd3b284e9e21e69c8ff98f1a90a02ef9eb975e4436d7eee2954a4 passes full Go/static original-runner checks, original17 comparisons, eight native update/partial-write controls and actual native→Go→native client capability/prekey/MLS state. Native reads identical Go full client/IDs and claims the Go-written prekey after clean restart; all13 process exits0/cleanup independently empty. Config uses native `setConsumableNotifications`; current live fixture disables queue setup, whose enabled lifecycle remains pending. This supplemental proof executes no original compiled Client test; those require Go client registration next. Registration/deletion/last-active/claims/federation and whole535/eight-Go gates remain pending; Brig is unavailable for selection and Galley follows whole completion.


[Public client registration and deletion](qualification/evidence/client-registration-public/README.md)
are now implemented at sourcea3980be43/servicefd9052f9 with full pinned Go
checks/static runner units. Registration binds actual account/code/storage/
peer/deletion/mail effects in native order; public deletion publishes native
client.delete and removes client/prekeys/configured lock while retaining MLS
rows. The [pinned Dynamo retry source alignment](qualification/evidence/client-deletion-native-retries/README.md)
remains distinct from native executable fault-injection proof. No original
compiled Client case or native registration/deletion switchback is claimed
yet. Those are next, followed by last-active and remaining whole-service
contracts. Earlier update/property receipts retain their own tested source
identities. Brig remains unavailable; Haskell remains supported/default.
