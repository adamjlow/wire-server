# Proxy reference characterization

This independent synthetic fixture runs the pinned Haskell image on a task-owned
Docker internal network, using a scratch-container provider and a generated
trusted CA. It captures actual responses and upstream attempts, not mock parity.
Every report says `parity_verified: false`. This initial baseline contains no Go
service observations.

The user-delegated environment decision is in
[the recorded review](../../../../../.migration/reviews/2026-10-07-self-review.md).
Source anchor: f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; provider routing/method/query
expectations come from `Proxy.API.Public` and the proxy route types. Do not change
the fixtures to make a candidate pass. Review observed unexpected source behaviour
separately before approving any comparison rule or generated golden.

Use the pinned Go 1.26.8 toolchain. From `oracle/provider`, run:

```sh
go test -race -count=1 ./... | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'
CGO_ENABLED=0 go build -o /tmp/wire-proxy-reference-provider ./... | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'
```

Inspect the compiler exit status (grep may return 1 for empty output). From the
repository root, run:

```sh
python3 -m unittest discover -s docs/go-migration/services/proxy/oracle -p 'test_*.py'
python3 docs/go-migration/services/proxy/oracle/capture.py --provider-binary /tmp/wire-proxy-reference-provider --output /tmp/wire-proxy-reference-capture
```

The output directory must not exist. No container ports are published: a
fixed-role client invoked through Docker exec sends requests inside the internal
network, with fixture control on container loopback. Profile bodies are bounded,
header inputs are allowlisted, repeated values
and raw targets are retained, and only synthetic data is permitted. The provider
has no dependencies beyond the Go standard library. Its ephemeral test
CA/private key must never be reused for production. Private key files are removed
with the temporary directory and are not written into capture artifacts.

Haskell trusts the generated CA using `SYSTEM_CERTIFICATE_PATH` and
`SSL_CERT_FILE`; `disableTlsForTest` remains false. The certificate-store override
is described in [the upstream x509-system source](https://github.com/vincenthz/hs-certificate/blob/master/x509-system/System/X509/Unix.hs).
Positive actual provider observations prove TLS succeeded; negative fixtures
include disallowed stream host and scheme. Root filesystems/mounts are read-only,
capabilities are dropped, no privileged/host-network container is used, and only
containers/network created by this invocation are removed. Existing services and
integration suites are untouched.

Initial cases cover all seven handlers, four raw-proxy method/query/502-retry
families, Spotify/Soundcloud inbound POST semantics, forwarding escapes,
upstream 404, endpath errors, missing URL, Soundcloud 302/failures, health/unknown
routes and V0..V19/unknown/internal version handling. This is baseline coverage,
not the complete verification matrix: transport failure/redirect/timeout/gzip,
disabled/development versions, secret reload, metrics/log fields, shutdown/load
thresholds and real candidate comparison remain required. Source-status mismatch
or incomplete capture returns nonzero and is recorded, never silently accepted.

Reports preserve full synthetic response headers and body bytes, provider effects,
actual image IDs, config/case/runner/provider hashes, CA public hash and provenance
method. A successful characterization exit is not proof of a complete contract,
authenticated live capture, binary rebuild, candidate parity or availability.

## First real candidate comparison

`live-65.json` captures both running services, and `comparison-65.json` records
65 matching cases after correcting TLS ALPN, mixed-case GET framing and shared
5xx error wrapping. This remains initial coverage, not full service approval.
The comparator reuses `tools/parity/compare.py` input/byte/header/type validators.
It permits only valid Date values within the capture window and equivalent
validated Content-Length/chunked framing. It preserves application headers,
ordered duplicates, trailers, bodies and all provider attempts exactly.
Negative tests reject altered status/body/headers/credentials/queries/effects,
invalid or ambiguous framing, invalid Date/weekday/window, missing cases and
changed stimuli. Supplied metadata is not authenticated by the comparator.

Same-author review under the user's explicit delegation: approve these narrowly
scoped comparison rules; they implement the contract's existing framing allowance
and permit no API/security exception. Successful initial comparison does not
approve missing middleware, operations, deployment selection or switchback.

Run `python3 .../oracle/compare.py CAPTURE_JSON NEW_REPORT_JSON`; it returns 0
for agreement in the declared cases, 1 for contract differences and 2 for invalid
input. Reports always retain `parity_verified: false`. Capture both processes by
adding `--candidate-receipt RECEIPT_JSON` to `capture.py`; receipts identify a clean
Go build, SHA256 binary and immutable local scratch image, whose extracted binary
is checked before launch. A local image ID is not a publisher registry digest or
independent build attestation. No live provider or production data is involved.
