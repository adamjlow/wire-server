# Optional Go federator

Whole-service process replacing federator when explicitly selected. Haskell stays
supported/default. Source reference and finite acceptance:
[contract](../../../docs/go-migration/services/federator/contract.md).

Build with pinned Go1.26.9 (`nix-shell go/shell.nix` or the verified official
compiler from go/toolchain.json), GOTOOLCHAIN=local/GOWORK=off:

```sh
cd go/services/federator
set -o pipefail
go build -o federator ./cmd/federator | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'
./federator -c /path/to/federator.yaml
```

Accepts existing upstream YAML, `-c` or `--config-file`; default
/etc/wire/federator/conf/federator.yaml matches original chart. Separate internal
and external HTTP1/cleartext HTTP2 listeners, brig/galley/cargohold forwarding,
per-use brig policy, DNS SRV wire order, verified outbound mTLS HTTP2 and hot
CA/certificate/key reload. Incoming identity is checked against discovered SRV
names; certificate chain trust remains the existing verified ingress boundary.
Response streams keep binary bytes, original errors/status and selected headers.
No database migration or shared Haskell/Go runtime. SIGTERM drains both listeners.
Common metrics retain labels/units; runtime gauges describe Go runtime statistics.
Go uses generic startup errors and does not log forwarded bodies; Haskell debug
body logging remains a documented inherited concern, not copied to HTTP errors.

From a clean repository, with the pinned compiler on PATH, build a local nonroot
image and a receipt (never pushes):

```sh
python3 go/services/federator/build_image.py --receipt /tmp/federator-image.json
```

To preserve an already qualified executable, supply --candidate and
--candidate-receipt from evidence/final/build.json (receipt identifies exact
binary hash, compiler and clean source). Mount existing config, cert/key and CA
paths read-only. Source federator chart has no exec preStop hook, so scratch image
needs no shell/helper. Docker smoke verifies default path, health200, nonroot/
read-only/capDropALL/no-new-privileges and SIGTERM0 in a private network.

Optional image selection uses existing upstream chart without edits:

```sh
helm template wire charts/wire-server -f YOUR_EXISTING_VALUES.yaml \
  --post-renderer go/services/federator/helm_postrender.py \
  --post-renderer-args YOUR_GO_FEDERATOR_IMAGE
```

The renderer requires exactly one federator Deployment/container and modifies
only its image. Without an image argument it returns bytes unchanged. Federation
must already be enabled in operator values; it never enables federation itself.
Missing/ambiguous targets or invalid images fail closed. Rollback selects the
original Haskell image with the same config and ingress; no stored state changes.

`python3 go/check.py federator` runs focused race/unit, format/tidy/vet/build,
module vulnerability and strict license checks. Original compiled77 run in the
owned Docker harness (five Go services and remaining Haskell peers); direct35
supplements cover gaps. The lightweight comparison lane additionally executes
real Haskell->Go->Haskell and one in-flight stream drain. See REVIEW.md and
SECURITY-NOTES.md for exact evidence, failed/aborted runs and security limits.
