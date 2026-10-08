# Additional inherited cargohold security concern

User disposition, 2026-10-08: “we must mirror how the backend works today but note
down any serious security concerns/improvements.” This applies to the observed
cancellation behavior below; no security fix is bundled with this replacement.
The earlier federation and parser concerns remain in the service oracle dossier.

## Unfinished multipart uploads after client disconnect

The real pinned Haskell process creates a multipart upload, uploads a 6 MiB part,
and leaves that upload open after an HTTP client disconnects partway through a
declared 24 MiB body. No completed object is created. The request logs its client
connection error, and the upload remains after the bounded 15-second observation.
The first Go candidate aborted instead. Exact reference/candidate effects and
runtime identities are retained in lifecycle-initial-observations.json and
lifecycle-initial-run.json. Task-only cleanup occurs after the state is recorded.

Repeated interrupted uploads can accumulate stored parts and storage costs if
there is no effective lifecycle cleanup. This is a service-boundary finding with
synthetic data in a private S3-compatible store; production cleanup policies and
exploitability have not been assessed. The migration preserves the same open
upload/part effect and 6 MiB buffering. It still aborts injected S3 part/complete
failures as the reference does; disconnect behavior is a distinct failure path.

Recommended separate improvement: ensure both Haskell and Go abort multipart
uploads when the inbound stream terminates exceptionally; review existing S3
AbortIncompleteMultipartUpload lifecycle policies and cancellation/abort retry
handling. Validate both runtimes under the same reviewed contract change, including
no completed object, no open upload and bounded cleanup after a disconnect.
