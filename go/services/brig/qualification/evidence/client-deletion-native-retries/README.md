# Client deletion Dynamo retry alignment

Production source `007248825f3b8e41ec5eb1b54274a61490b6c6e6`, executable
`e4372307f57fc9455386368a8443f13d878f8814a9ceafeba3ccc28e0c9c1bd0`,
passes full pinned Go1.26.9 SDK checks and unchanged-runner normal static
units. Tagged stores compile only. Two managed container exits0/network
removal0 and independent exact-name cleanup audits empty. Existing static
glibc/OpenSSL linker warnings retained; no native Haskell rebuild.

The exact Amazonka revision in current and frozen native reference flake.lock
is identical. [SDK sources](pinned-sdk-sources.json) record immutable primary
URLs and downloaded hashes. HTTP.retryRequest/retryService, Env default
retryConnectionFailure, DynamoDB.Types.defaultService, Error.parseJSONError
and Types.ErrorCode supply the source contract; no newer SDK default assumed.

Service errors retry at most5 with deterministic25/50/100/200/400ms delay.
The source-selected HTTP statuses and400 error names are implemented, with
body __type/code precedence, fallback header handling, namespace/single
Exception-or-Fault suffix normalization and message fallback. Malformed
error JSON remains a nonretryable serialization failure even at500/503.
Explicit empty error code does not accidentally inherit a header code.
Fresh requests are signed each attempt; cancellation stops backoff. Native
connection policy caps selected transport retries3 and excludes response
timeout; Go maps dial/no-response/closed-connection classes to that boundary.
Dynamo's successful304 handling follows native statusSuccess.

Finite controls cover status/code/message/header/normalization classifications,
exact backoff values and connection/response-timeout distinction. Actual
signed HTTP failures→success use3 calls; permanent selected failures use
exactly6 calls and return error. Prior deletion route/order/SQS/lock/local-mode
controls remain passing in this complete normal/race/static unit run.

This removes the known single-attempt implementation gap by aligning the
pinned source contract. It is not a native executable fault-injection proof:
actual Dynamo/native deletion state switchback, default-region/TLS/timeouts,
InternalException/error-class mapping, redirect and broader response schema
comparisons remain unqualified. Original Client tests/full535/eight-Go were
not run. Full registration/mail/code/config composition, queue consumer/
STOMP/last-active and all remaining whole-service gates remain pending.

Same-author slice review under explicit user-delegated self-approval accepts
the bounded source-alignment change with the above qualification limits,
not independent or whole-service approval. Generic errors omit credentials/
response bodies, no dependency introduced and no Haskell/oracle/default/schema/
security-policy repair. Preserve native partial deletion and retained MLS rows.

After terminal cleanup, a completed own earlier registration-core artifact
directory was relocated to a separate owned runtime temporary filesystem.
[The manifest](completed-artifact-relocation.json) verifies all19 original
files' hashes/uid/gid/mode, preserves logical paths through a parent-directory
symlink and retains every binary/receipt. It reclaims204887700 shm bytes;
no cache or evidence was pruned and other resources were untouched.
