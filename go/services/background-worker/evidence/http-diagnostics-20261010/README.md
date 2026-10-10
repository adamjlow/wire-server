# HTTP timeout and stored diagnostics construction (2026-10-10)

Source/reference identities and passing command receipts are retained here. Full
Go1.26.9 checks pass on source-files.json, including race tests. The source worker's
ordinary manager uses http-client0.7.19 (identified in the pinned native ELF via
readonly no-network discovery); primary tagged Core/Response/Manager source shows
30s connection acquisition followed by remaining-budget response-header parsing,
without a response-body deadline. Go now tracks those phases and caller cancellation.
Scaled real HTTP tests cover stalled headers, body completion after the header
budget and parent cancellation after headers. There was no fresh native execution
of the timeout policy. Existing callback/federation transports are unchanged.

Native Arbiter classifyException preserves handler errorMsg through retry/DLQ.
Go now passes its handler error text into the guarded native SQL rather than
replacing every cause with one constant. Production domain/storage adapters return
generic errors; panic values remain unformatted. Different runtime exception text
is not asserted as byte identity. Existing executor tests verify diagnostics reach
both retry and DLQ. The reused original-schema PostgreSQL component fixture verifies
actual persisted nonconstant text and existing attempts/lease/ACK/stale/rollup guards.
It passed; every owned container/volume/network was removed with cleanup0 and empty
owner queries (docker.json). This is component evidence, not whole-service E2E.

Inherited concern: native unknown exception rendering may persist sensitive backend
or payload details in last_error; future handlers must not return such values in
Go errors. The implementation preserves diagnostic storage, without introducing raw
panic/request/SQL logging or changing native schemas. Native retry classification
for the two registered handler families remains retry-unless-gone/cancelled.

The remaining ordinary-manager source audit explicitly includes environment proxy
semantics and reused-connection replay/header parser failure behavior. These checks
do not claim those gaps are resolved. No contract exception or whole-service review
is granted. Native Haskell build checks unavailable/unrun; original47/six-Go124,
SCIM repair, operations, actual durable switchback and final image/selector remain
required. Same-author bounded source/storage review checked these inputs under the
existing user delegation; full task and compatibility gates remain unsatisfied.
