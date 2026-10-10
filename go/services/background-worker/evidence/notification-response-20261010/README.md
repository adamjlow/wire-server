The actual return-Haskell rehearsal rejects fixture200/missing-media as
UnsupportedContentType and retries. Its primary BackendNotificationAPI is
Post '[JSON] EmptyResponse; EmptyResponse.FromJSON accepts any object. The
FederatorClient RunClient respects expected status200. These source identities
and the native failure are retained. Candidate regression before repair proves
all six invalid success/media/status/body cases were incorrectly accepted.

Repair: used federation requests accept status200 and application/json media,
including ordinary MIME parameters; notification delivery additionally requires
a JSON object, ignores its fields, and keeps generic errors retryable without
ACK. Failure responses still use the existing federation error/drop decoder.
The corrected candidate-unit HTTP2 positive fixture now declares JSON instead
of Go's automatic text/plain; protected/original tests are untouched. Focused
response cases and real consumer wrong-media-then-valid retry/oneACK run in the
full pinned Go1.26.9 checks (exit0, format/tidy/verify/vet/unit/race/build/security/
license). No dependencies change, no speculative parser-identity suite added.

The qualification sink's successful response is now200/application/json/{}.
The shipping executable changed, so old89f24e7a original124/image/operations are
historical evidence of that executable. Current exact rebuild/final124/optional
image/operations/durable switchback and selector/review remain required.
