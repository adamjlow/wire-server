# Optional Go cannon

Candidate on `go-migration`; final original-suite validation is pending.
Haskell remains supported and default. This replaces the whole cannon process;
Cassandra schemas, RabbitMQ queue names/effects and gundeck contracts stay shared.
The finite plan and protected contract are under `docs/go-migration/services/cannon`
and `docs/go-migration/plans/cannon.md`. Exact qualification and limits are in
[delegated review](REVIEW.md).

Use pinned Go1.26.8 and the existing upstream YAML configuration and RabbitMQ
credential environment. From this module:

```sh
go build -o cannon ./cmd/cannon
./cannon --config-file /etc/wire/cannon/conf/cannon.yaml
```

`-c` is the short config option; the path above is also the default. Existing
Cassandra, RabbitMQ and gundeck remain required. Classic await/websocket presence,
raw/bulk push and consumable events/ack/full-sync/markers use the existing network
and storage protocols. TLS verification is required: Go explicitly rejects
`insecureSkipVerifyTls=true`. Keep Haskell for deployments requiring that option
until its contract/security disposition is resolved. No unsafe TLS-mode parity
is claimed. Serious inherited concerns are separately recorded in the contract.

Full checks: `python3 go/check.py cannon` from repository root with the pinned
compiler on PATH. This runs format/tidy/module verification/vet/race/build and
vulnerability/license checks. Security regressions cover strict typed acknowledgements,
atomic bulk validation/integer precision, generation-safe cleanup and trusted TLS.
Dependencies are pinned per module; no Go tooling is added to the Haskell workflow.
The module is AGPL-3.0-or-later.

The optional local container command requires a clean committed checkout:

```sh
python3 go/services/cannon/build_image.py --receipt /absolute/new-image-receipt.json
```

It builds a non-root scratch image with the tested binary and a verified CA bundle
from the qualified publisher image, records immutable identities, and never pushes
or deploys. Select your own published image through your existing Helm invocation:

```sh
--post-renderer ./go/services/cannon/helm_postrender.py --post-renderer-args YOUR_CANNON_IMAGE
```

This changes only the cannon container in the cannon StatefulSet. The nginz
sidecar, configurator init container, volumes, config, arguments and original test
hook remain. Omitting selection returns byte-identical manifests; invalid or
ambiguous targets fail closed. Existing chart sources are unchanged. A chart image
values overlay would also replace the Haskell test image, which is why the selector
operates on the named service container in the render.

Original Haskell tests run automatically in the authorized private Docker harness,
with exact binaries selected before static/dynamic backend startup. The finite
qualification includes the original35 cannon assertions, 10 HTTP/7 config/28
uncovered boundary records, queue/Cassandra Haskell↔Go switchback, bounded loaded
drain/smoke and the original67 with all three Go components plus Haskell peers.
`cmd/parity-fixture` is a synthetic private verification utility; the production
image contains only `cmd/cannon`. Never run it against shared or production stores.
Retained failures are part of the evidence; a failed run does not qualify support.
Future upstream changes require the repository’s existing sync/requalification
process. Rollback selects the original whole Haskell process/image using the same
queues and schemas; no production switch is performed by this work.
