# Optional Go gundeck

Whole-service Go gundeck is qualified for the exact versions and mixed deployment
in [REVIEW.md](REVIEW.md): 72 original e2e tests, 91 frozen comparisons, actual
persisted Haskell↔Go switchback, security/quality and optional packaging checks.
Haskell remains supported and default. Availability does not authorize a
production switch or qualify arbitrary future revisions/peer combinations.

The module provides Redis presence, Cassandra notifications, websocket/native
push delivery, SNS token ownership, SQS feedback and consumable RabbitMQ queues.
It preserves existing schemas/configuration and reads legacy Redis fields/URI
values. Presence API DELETE retains Haskell's no-op; delivery cleanup compares
stored generations before deleting. See [security notes](SECURITY-NOTES.md).

Use pinned Go1.26.9/GOTOOLCHAIN=local/GOWORK=off. The service entry point is
cmd/gundeck; check this module with `python3 go/check.py gundeck` from the repo root.
Supply the existing config with `-c`/`--config-file`; default path is
`/etc/wire/gundeck/conf/gundeck.yaml`. Existing environment credentials and verified
TLS/custom CAs are supported. Insecure TLS verification bypass is rejected.
Client IDs follow the source Word64/canonical lowercase encoding. Empty SQS polls
default to the source 500 ms pause. Response compression follows
[wai-extra's defaults](https://github.com/yesodweb/wai/blob/master/wai-extra/Network/Wai/Middleware/Gzip.hs).

`build_image.py --receipt NEW_FILE --candidate IDENTIFIED_BINARY --candidate-receipt BUILD_RECEIPT`
packages an identified service binary unchanged with the separately compiled
lifecycle helper and pinned reference CA bundle. It requires a clean checkout and
pinned compiler, validates compiler/source/hash receipts, and never pushes/deploys.
Without candidate arguments it builds current service source, which needs its own
qualification. The image runs as UID65534. Its `/bin/sh` helper implements only
existing Helm `sh -c "sleep 10"`; other shell commands fail closed. There is no
general shell or embedded Haskell runtime.

`helm_postrender.py` reads a complete existing Wire Helm render on stdin. No image
argument preserves its bytes; an explicit image argument selects only gundeck's
container image and rejects missing/ambiguous/changed deployment shapes. Source
charts, other services and Haskell defaults remain intact. Choose an operator
published image reference for deployment; the retained image ID is local only.

`core_driver.py --four-go` combines the unchanged original72 with frozen91 and
real persisted switchback in the authorized private Docker harness. Supply exact
binary/build receipts for gundeck and all three selected Go peers; resolve peer
versions from current migration compatibility records rather than old cache names.
The root runner requires clean committed source, internal networks/no host ports,
synthetic fixtures and owned cleanup. The implementation never modifies protected
assertions or reference captures to obtain parity. Exact commands and results are
in the plan/task and retained [final evidence](evidence/final/environment.json).
