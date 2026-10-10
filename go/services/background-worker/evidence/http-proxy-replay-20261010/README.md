# Ordinary HTTP proxy and reused-connection behavior

http-client0.7.19 primary source identities are retained in reference-files.json.
The ordinary manager now snapshots case-folded proxy variables with exact lowercase
precedence, http/bare proxy URLs, synthetic Basic authentication, literal no_proxy
domain suffixes, no wildcard/port rules and no implicit localhost bypass. Invalid
proxy configuration fails scheduled startup before health and returns generic
credential-free errors. No backend schema/API/config key changed.

A reused connection's EOF/incomplete-header or network I/O failure is retried only
before a response is returned. The discarded connection is reacquired with the
native per-attempt connection/header budget. Fresh failures, invalid status lines,
timeouts, cancellation and one-shot chunked Gundeck bodies are not replayed.
The native source explicitly forbids application retry of Gundeck's consumed
chunked body; byte-preserving one-shot requests remain unchanged.

Real fixtures prove proxy routing/authentication and environment snapshots,
recovery from truncated headers on a reused connection, and fresh/one-shot/invalid
status negatives. Existing timeout/body/cancellation checks still pass. Full
Go1.26.9 format/tidy/verify/vet/unit/race/build/vulnerability/license checks pass in
[go-check.log](go-check.log); dependency assembly/license inspection limitations
are retained in that log. Only the malformed-status test stimulus subsequently
changed; focused TestInternalHTTP race execution passes on the final test source.
Existing dependency/build/security results apply to unchanged production code.
Source hashes identify the exact code/test contents.

Same-author source review under existing user delegation checks proxy credential
confidentiality, context/transport cleanup, retry eligibility and existing domain
bindings. This component proof is not final whole-service qualification.
The [finite ordinary-manager review](ordinary-manager-review.md) documents
native fragment-dependent line/count guards and lenient malformed-line handling
versus Go's standard parser. Used valid Brig/Gundeck interactions are preserved;
no arbitrary malformed-wire or http-client library replacement identity is claimed.
Original/service contract failures remain failures; no new acceptance gate or
exception is introduced for incidental parser identity.
Final sixGo124/operations/durable switchback/image/selector remain pending.
