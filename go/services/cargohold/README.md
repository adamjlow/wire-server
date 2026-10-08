# Optional Go cargohold candidate

Work in progress against the approved whole-service contract. Haskell is still
the supported default. This first runnable slice implements bot/provider
upload, S3 multipart persistence, signed download and deletion; it is not a
complete or verified replacement. User/legacy/federation/token/audit/CloudFront,
full configuration/version middleware, logging and operational parity remain.

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

Next: user/Brig and token/versioned routes, audit metadata, legacy/federation,
CloudFront/config variants, exact parser errors and middleware/operation behavior,
then the original suite and the already bounded supplemental/switchback matrix.
