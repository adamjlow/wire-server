# Finite-DHE bot transport construction (2026-10-10)

Source inputs and reference file hashes are recorded in `source-files.json` and
`reference-files.json`; `result.json` identifies the parent commit, compiler and
actual static test executable. This was an identified implementation worktree,
not a clean final service executable or whole-service qualification.

Go first negotiates its existing pinned ECDHE/TLS1.3 connection. Only the remote
TLS handshake-failure alert permits a finite-DHE TLS1.2 fallback using the trusted
OpenSSL executable. Authentication failures never fall back. The actual connection's
RSA fingerprint, CA/self-signature and dates are verified by the existing mandatory
Go verifier before net/http receives the stream. HTTP credentials/body cannot be
sent during the pre-authentication handshake. The helper receives fixed argument
vectors, no shell or backend environment credentials; peer diagnostics are discarded.
Transcript memory and handshake time are bounded. Closing or cancelling the stream
kills and reaps its helper; existing callback slots bound concurrency.

Full final-input Go1.26.9 format/tidy/verify/vet/race/unit/build/module-security and
license checks passed (`go-check.log`). Original callback request/retry/cancellation
unit tests are reused. New real TLS tests establish finite-DHE-only negotiation and
HTTP exchange, wrong-pin/expiry rejection, stalled-handshake cancellation, invalid/
oversized transcript rejection and no fallback after Go authentication failure.
These are synthetic component tests, not execution of the original Haskell E2E.

`patched-container.log` is the passing static test executable in the immutable
runner with OpenSSL3.5.9, readonly root/artifacts, nonroot user, no network/host ports,
dropped capabilities, no-new-privileges and bounded memory/CPU/PIDs/tmpfs.
The patched OpenSSL source archive matched its publisher SHA256; `openssl-download.json`
and `openssl-build.json` identify source and isolated native build. `build-openssl.py`
is the exact build recipe (its owned build directory must be fresh). The build ran
no upstream OpenSSL tests (`no-tests`); we rely on the publisher release and these
specific interoperability checks. Raw compiler log remains in the external cache,
with its digest recorded. OpenSSL's Apache2.0 license is retained in third_party.
The old runner3.6.2 and host3.0.13 tests are fixture observations only, not accepted
production dependency versions. `runner-container.log` is an earlier construction observation. Final patched
receipt covers the no-fallback test and the corrected transcript parser, including
long subject lines without an incidental4KiB buffer limit.

Primary references: [s_client documentation](https://docs.openssl.org/3.5/man1/openssl-s_client/),
[publisher releases/support](https://openssl-library.org/source/) and
[29September2026 advisory](https://openssl-library.org/news/secadv/20260929.txt).
Current3.5.9 fixes the published issues; existing runner3.6.2 is older. Final optional
image packaging must select a patched supported CLI, retain its license and verify
this stream adapter against the actual image. Module-level govulncheck does not scan
an external CLI; final dependency review must cover that packaged component.

The first focused probe failed because the parser expected an extra blank line.
That parser bug was fixed without weakening certificate verification; the observed
failure is recorded in result.json (the iteration overwrote its original raw log).
All owned build/test containers returned0 and were removed, and exact owner-label
queries returned empty. `construction-cleanup.json` records removal of only the
completed reproducible compile tree, keeping the tested install, verified source
archive, reference artifacts and receipts.

Same-author bounded review: checked pre-HTTP authentication, restricted fallback,
process cleanup, unchanged native pins/schemas/assertions and exact fixture identities.
This is construction review under existing delegation, not independent review or
whole-service approval. Native Haskell build checks remain unavailable/unrun.
Final original47/six-Go124, SCIM repair, operations, durable switchback, image/selector
and final review remain required. Haskell remains supported/default; task/goal active.
