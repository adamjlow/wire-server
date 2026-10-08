# Cargohold candidate review

Reviewer: codex (self-review delegated by user), the same actor as the author.
Authority is the user's explicit self-approval request and subsequent autonomous
development instruction. Security disposition is the user's explicit instruction
to mirror the backend today and record serious improvements separately.

The finite acceptance matrix passes for candidate
`68a36b9137ce26566b2db91902469e8159885656`, binary SHA256
`d3591c8f7cbbb5ecbbf9c1425feb29c2b1867e270156337a0cf639eee43f7467`:
27 unchanged original compiled tests, no skips/errors, with Haskell peers; 112
[supplemental records](../../../tools/parity/cargohold/supplemental/final-report.json),
including actual mixed federation, bidirectional user/bot/provider storage and
audit switchback, parser/security negatives, signed reads, injected S3 failures,
configuration variants, operations and lifecycle. This establishes the declared
bounded contract validation, not exhaustive equivalence over all possible inputs.

[Original report](evidence/final-e2e-run.json), XML and
[environment receipt](evidence/final-e2e-environment.json) retain exact source,
binary, fixture and dependency identities. Test and cleanup exit codes are zero.
The qualified all-Haskell baseline and trusted publisher provenance are retained
in tools/parity/docker/baselines. No independent Haskell rebuild is claimed.

[Quality output](evidence/metadata-quality.log) passes pinned Go 1.26.8 formatting,
tidy, module verification, vet, race, build, vulnerability and license checks at
ffb5ccda3f6429067866b6b08b811effd9173bac. The only subsequent Go source change
is a stdlib-only regression test checking malformed metadata has zero storage
calls. The entire module's race tests were rerun successfully after adding it;
production code/dependencies remain identical. Historical quality/e2e reports
are preserved without relabeling their revisions.

[Final image](evidence/final-image-build.json) contains the exact tested binary,
with CA provenance and non-root, read-only, no-network startup/idle SIGTERM smoke.
The optional Helm post-renderer preserves original tests and defaults: full
[render comparison](evidence/helm-selection.json) changes only the cargohold
Deployment image. Missing/ambiguous/wrong-container targets fail closed. Packaging
sources and upstream charts have not changed since that render evidence.

Operations meet the fixed 192 MiB RSS and Haskell-relative p95 limits with
concurrency two and 40 requests per runtime. Actual 24 MiB signed bytes are
verified. Application metrics definitions/labels/counts match. During SIGTERM,
new requests fail, the active stream drains completely, and exit is zero within
30 seconds. Cancellation preserves the observed reference's unfinished upload;
the serious inherited concern is documented separately.

Code review covered routing/version/principal boundaries, strict input parsing,
token operations, S3 streaming/copy representation, signing, federation h2c/version
negotiation, generic external errors, log/metrics fields and signal handling.
No reference assertions were weakened. Known inherited federation and multipart
concerns are in the protected security notes; fixes require separate reviewed
changes to both backends. The migration retains verified TLS, synthetic fixtures,
bounded tests and pinned checked dependencies.

Haskell source, builds, schemas, charts and omitted-selection defaults are
unchanged. Nix/GHC/Cabal are unavailable locally: full Haskell compilation/unit
checks were not run or claimed; unchanged Haskell retains upstream CI. Commits
are local on go-migration; no push, default change or production deployment.
