# Optional Go background-worker

Whole-service Go replacement for the background-worker at reference
`f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`. Haskell remains supported and default.
The [final review](REVIEW.md) records exact tested source/binary/image, finite
contract coverage, same-author approval and security/operating limitations.

The service includes federation notification delivery and queue discovery, DLQ
watching, Rabbit group sync, both scheduled adminless/meeting pools with native
schema/leases/cron/reapers/listener, and all seven migration families. It uses
existing configuration, network, queue and storage boundaries; no embedded Haskell
runtime or schema fork is required.

All **124 original Haskell integration executions** pass with the six identified
Go services and remaining Haskell peers, including all47 worker cases. The
[final receipts](evidence/final-original124-20261010/) retain strict named XML,
selected hashes and successful owned cleanup. Pinned Go quality/security checks,
[finite operations](evidence/operations-final-20261010/) and actual same-state
[Haskell→Go→Haskell recovery](evidence/switchback-final-20261010/) pass. Historical
failures remain in evidence; no case was skipped or waived. The first reference
handover deliberately holds a SQL row lock and requires a bounded crash stop;
Go and returning Haskell stop cleanly. See review for the precise scope.

Run `python3 go/check.py background-worker` from the repository root with pinned
Go1.26.9 on PATH and `GOTOOLCHAIN=local GOWORK=off`. Build from this module with
`CGO_ENABLED=0 go build -trimpath -o /absolute/output/background-worker ./cmd/background-worker`.
The executable accepts `-c`/`--config-file` and `--galley-config-file`, defaulting
to `/etc/wire/background-worker/conf/background-worker.yaml` and
`/etc/wire/galley/conf/galley.yaml`. Supply Rabbit credentials through existing
`RABBITMQ_USERNAME` and `RABBITMQ_PASSWORD` variables. Keep both existing config
mounts, trusted dependency CA and service identity. Diagnostics omit credentials
and payloads; inherited policy concerns are documented in REVIEW.md and the plan.

The optional scratch image packages the static worker, supported patched static
OpenSSL3.5.9, trusted reference CA and license notices; it runs as UID65534. The
helper preserves source finite-DHE bot compatibility and mandatory pin/signature/
date checks before HTTP. Build helper dependencies only in Docker:

```sh
python3 go/services/background-worker/build_openssl.py --output /absolute/new/cache-directory
python3 go/services/background-worker/build_image.py --output /absolute/new/image-receipt.json --candidate WORKER --candidate-receipt BUILD.json --helper OPENSSL --helper-receipt HELPER-build.json
```

Run the image packager with pinned Go on PATH and a clean checkout. Optional
`--archive` on the helper builder reuses publisher-checksummed source. These tools
build local artifacts and retain provenance; they do not publish or deploy.

The [optional Helm postrenderer](../../../tools/parity/background_worker_helm_postrender.py)
reads the existing render on stdin: no argument preserves default bytes; an image
argument changes only the worker image and rejects unexpected deployment shape,
missing config mounts or non-Recreate strategy. Use a published immutable image
reference only through the deployment's normal authorized change process. The
qualified local image is identified in REVIEW.md; it has not been pushed.

[Machine records](../../../.migration/services/background-worker.json) own current
availability and compatibility pins. Reassess those pins after upstream changes,
following the [sync procedure](../../../docs/go-migration/UPSTREAM.md). Host
GHC/Cabal/Nix checks and compiled native14 units were unavailable; this is recorded,
not a native build pass. Existing source14 expectations run in Go, and the actual
published Haskell original integration executable supplies integration evidence.
