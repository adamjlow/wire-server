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
