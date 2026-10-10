# Owned harness preparation (2026-10-10)

Source checkpoint: `972e97540c14d6087a2b885b6c631b260ee8edb8`.
This is environment preparation and documentation reconciliation, not service
qualification or a test pass. Haskell stays default; the goal/task remain active.

`cache-cleanup.json` records SHA256 and size of 243 reproducible Go build-cache
entries removed after checking exact worker module identity, regular-file path,
size/mtime and absence of live Go builds. Current peer/static binaries, historical
receipts and reference artifacts were preserved. The removal recovered
1,312,280,776 bytes; available space rose from 1,183,236,096 to 2,495,987,712 bytes,
above the Docker harness 2 GiB preparation floor at this observation. Subsequent
builds may consume space; this receipt is not an ongoing capacity guarantee.

Read-only inspection of pinned runner image
`sha256:cd03212430eef6cfde0324b50885c12980ee211483ab27dde6ee730252966cd2`
found executable
`/nix/store/zyrxhd7nwmkcs11m144jagxcmddw2i41-openssl-3.6.2-bin/bin/openssl`.
Its `version -a` reports OpenSSL 3.6.2, library 3.6.2, 7 April 2026. Both discovery
containers used no network, no writable root, user 65532, dropped capabilities,
no-new-privileges, an explicit owner label and `--rm`; commands exited 0.
This establishes tool availability only. No finite-DHE fallback, dependency
security approval, optional production image or TLS compatibility is claimed.

Retained SCIM failure logs identify Brig's 500 propagated through Spar during
`runPhase2`, before user migration activation. They do not identify the root cause
or qualify enabled user migration. The original assertion remains required.

Documentation/record checks: `make migration-check migration-test` passed
(32 validator tests); scoped validation against the source checkpoint passed,
local Markdown targets resolve, and `git diff --check` passed. Post-discovery
owner-label query returned no containers. Same-author review checked updated
claims against existing receipts and the active/null-compatibility service record;
it does not approve the unfinished whole service. Go/Haskell source and dependencies
are unchanged, so prior source checks remain reusable and no service rebuild was run.
