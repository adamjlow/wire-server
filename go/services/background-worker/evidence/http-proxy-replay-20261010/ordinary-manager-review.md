# Finite ordinary-manager audit

Same-author review under the continuing user delegation. This closes the existing
ordinary-manager audit at the frozen service contract's scope; it adds no exception
to original assertions, API/configuration/storage or finite acceptance criteria.

Used ordinary bindings are Brig GET /i/users for complete account decoding and
Gundeck one-shot chunked POST /i/push/v2. They use HTTP endpoints and consume status
and JSON/body outcomes, not arbitrary custom response metadata. Native timeout
phases, body/caller cancellation, proxy environment snapshots/domain suffixes and
reused EOF/incomplete-header recovery now have finite source-based/real fixtures.
Fresh/status/cancellation/one-shot negatives are retained. All domain payloads,
request headers and endpoint/config selection remain governed by existing source
and original tests. The bot pin/DHE and federation HTTP2 transports are separate.

Go retains its standard HTTP parser. Native http-client0.7.19 ignores colonless
header lines and defaults to100 header fields; its4096 line guard checks accumulated
chunks only when LF is absent, so it is fragmentation-dependent rather than a
strict4096 complete-line limit (socket reads8192). Go rejects malformed HTTP header
syntax and has a bounded aggregate response-header limit. These helper parser
behaviors are not byte-identical. They are documented implementation/security
differences for nonstandard peer responses, not evidence of identical malformed
wire syntax or a relaxed source assertion. No source-shaped bespoke HTTP parser is
introduced solely to reproduce malformed syntax or fragment-dependent limits.

This decision follows the explicit finite-acceptance instruction to avoid
exhaustive incidental/runtime identity, while preserving standard valid HTTP
interactions on the inventoried source service boundaries. Do not represent the
migration as a general http-client replacement or arbitrary malformed-wire
compatibility guarantee. Any actual original/domain mismatch remains a failure.
Native Haskell helper tests are not claimed run; primary source URLs/hashes are
retained in parser-reference-files.json. Final original124, operations, durable
switchback, optional supported OpenSSL image/selector and final service review
remain required. This audit is not whole-service qualification.
