# Optional Go cargohold candidate

Work in progress against the approved whole-service contract. Haskell is still
the supported default. The candidate implements user/bot/provider upload,
S3 multipart persistence, signed download/delete, tokens, versioned routes,
legacy reads, federation availability/streaming and outbound h2c, audit metadata
and CloudFront signing. It is not yet a verified complete replacement. Audit
logging, exact parser/failure behavior, full configuration/operation parity,
packaging and the remaining finite supplemental/switchback checks remain.

Use pinned Go 1.26.8 and the existing upstream cargohold YAML:

```sh
go build -o cargohold ./cmd/cargohold
./cargohold --config-file /path/to/cargohold.yaml
```

Credentials use the standard AWS SDK provider chain. TLS verification is enabled;
uploads buffer one 5 MiB S3 part at a time. Existing accepted delimiter/MD5/length
suffix/trailer quirks and completed partial-object effects are preserved.
Parser error text and all remaining contracts still require direct reference
comparison. Unit tests prove candidate behavior only. Dependency choices and
the finite acceptance matrix are in the implementation plan.

## Initial executed evidence (2026-10-08)

At source ef75a86972653684f3e1a00ece26f9fad5746f83, Go/check.py cargohold
passes gofmt/tidy/verify/vet/race/build/module-vulnerability/license checks.
The license reporter warns that this local unpublished module's version is HEAD;
the checked repository LICENSE is AGPL-3.0. This is not a dependency vulnerability.
The three candidate unit tests check accepted parser quirks, malformed headers,
limits/partial-object effects and principal/token/mutation boundaries.

A static binary (CGO_ENABLED=0) ran the private real-S3 smoke:

```sh
python3 go/services/cargohold/smoke.py --binary /absolute/cargohold --output /absolute/new-directory
```

[Six-case result](evidence/initial-slice.json) and
[run identity](evidence/initial-slice-run.json) record successful uploads for
canonical/unequal delimiter/wrong MD5/length suffix/missing trailer, signed URL
reads with actual byte equality, deletion effects, and incomplete-body status
400 with the same completed 10-byte object as the Haskell capture. No open multipart
uploads remain. The process exited zero and all owned containers/networks were
removed; Docker inspection confirmed no resources with its unique prefix remain.
Inputs include the existing original integration configuration and reference
captures. This smoke reuses their expectations read-only, leaves protected oracle
files untouched, and explicitly marks whole_service_parity false. It does not
execute the original 27 cargo tests or replace whole-service verification.

## Original suite execution

At source f410fca816fdca360720ca3565143a88a49185a7, the unchanged original
compiled cargohold suite passed all 27 selected functions with zero failures,
errors or skips. Go replaces cargohold (static and dynamically spawned processes);
all other service executables remain Haskell. This means the two cargo domains in
that suite both select Go; mixed Haskell/Go cargohold federation is still a
supplemental requirement, not established by this run.

[Original XML](evidence/core-e2e.xml), [suite run](evidence/core-e2e-run.json) and
[environment identities](evidence/core-e2e-environment.json) retain exact binary,
images/config/fixtures and process/cleanup exits. Binary SHA256:
6b2358eaec0c5b8a43797890e8293e47fa8a4228a344cc6af85baec05cfd8aa4.
The owned controller stack returned pass/exit 0/cleanup 0; subsequent Docker
inspection confirmed its unique containers/networks were removed. Retained
Haskell baseline uses the same fixture hashes. This is real original-assertion
reuse, not translated test specifications or a whole-service availability claim.

Subsequent candidate changes validate required configuration fields and supported
version/addressing choices, implement CloudFront RSA-SHA1/300-second canned-policy
signatures, align domain case folding/validation and exhausted HEAD failure
behavior with source. They have focused security/crypto/config tests; the original
27 report above applies to its recorded binary, not silently to later code.
Next finish audit logging/parser/error/operational parity, packaging, and the
already bounded supplemental/mixed-runtime/switchback matrix. Revalidate the
final binary through the original suite once those relevant changes settle.

## Supplemental comparison milestone

At candidate code bcbcd03b01f22a9f2e751b4c7385467a5912d1e3, 25 reference-owned
supplemental comparisons match the actual Haskell process and the Go binary in
the same disposable S3 environment. These include nine exact multipart error
messages/positions and residual effects, token header precedence, non-owner token
mutation, malformed token/asset syntax, legacy reads, internal signed URL reads,
audit-required metadata failure and six bidirectional user/bot/provider storage
switchbacks. User token renewal through the other runtime rejects the old token
and accepts the new token on the original writer. The protected supplemental
report explicitly excludes a whole-service parity claim.

A stricter signed-URL normalization check was then run at controller source
7044e09e43f0716400ffd5bb046a5d9ac6196f06 against the same unchanged candidate
binary: all 25 cases still match. It retains every fixed signing query field,
verifies actual signed bytes, validates signing algorithm/date/credential scope
and only normalizes verified date/signature values. No owned Docker resources
remain after either run. The original 27 report applies to its older recorded
binary; the final binary still needs original-suite revalidation.

Audit logging and request ID middleware now emit the reference uploader/downloader,
conversation/file/URL fields and supported configured log formats. Candidate
parser/JSON content types and malformed-token behavior were corrected against
real reference observations. [Current Go quality output](evidence/quality.log)
records successful gofmt/tidy/verify/vet/race/build/vulnerability/license checks
at source 7044e09e43f0716400ffd5bb046a5d9ac6196f06 (unchanged Go code since bcbcd).
Remaining work is the declared audit/config/failure/mixed-federation/operational
supplement, packaging/selection and the final original-suite run and review.
