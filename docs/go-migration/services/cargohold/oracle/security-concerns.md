# Cargohold security concerns and improvements

Disposition (user, 2026-10-08): mirror today's backend behavior during the Go
migration and record serious concerns separately. No security fix is part of this
language replacement. Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e;
actual isolated service observations and runtime provenance: reference.json/run.json.

## Serious: foreign-origin owner UUID bypasses private asset token

Confirmed on federation availability and streaming: a foreign-domain requester
using the stored owner's UUID succeeds without the private asset token and with
a wrong token. A different UUID correctly requires the token. The domain is lost
when API.V3.checkMetadata takes qUnqualified from the qualified requester and
compares it to an unqualified stored principal. Exact positive/negative cases are
in reference.json. This is a cross-domain identity-confusion concern that could
allow private asset access if a reachable trusted federation request can supply
the owner's UUID. Deployed ingress/federator reachability and exploitability have
not been assessed; do not equate a unit/service boundary finding with a demonstrated
production attack. No production credentials or customer data were used.

Migration decision: retain this behavior exactly, including negative controls.
Recommended separate fix: compare the complete qualified owner identity, deriving
the stored local owner's domain from the backend configuration, while preserving
legitimate owner/public/token access. Apply and validate the same reviewed change
to Haskell and Go, assess federation clients and rollout compatibility, and add
foreign-domain same-UUID absent/wrong-token rejection regressions on both routes.
Track through a separate authorized contract/security task; not silently in Go.

## Context-dependent: legacy OTR ignores requester/uploader equality

Confirmed reads for same and different requesters when stored uploader is
parseable; absent/malformed uploader is rejected. Assets are shared encrypted
objects, so an uploader-only policy is not established by this observation.
Review expected conversation membership and upstream ingress enforcement before
proposing a change. Preserve current behavior in the migration.

## Integrity/resource improvements requiring separate compatibility review

The multipart parser accepts an unused Content-MD5, unequal boundary lines,
numeric length suffixes and a missing trailer. A short body returns 400 after
committing the available bytes as an object. These are confirmed behaviors;
no production data-integrity incident or exploit is claimed. A separate proposal
may validate checksums/delimiters and abort incomplete uploads, considering old
clients, object lifecycle cleanup and both runtimes. Do not assert stricter Go
parsing is compatible or hide the current persisted partial-object effect.
