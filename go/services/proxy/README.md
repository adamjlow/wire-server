# Optional Go proxy

This is a separate Go executable implementing the proxy provider routes,
configuration, ordered queries, secrets reload, API versions, gzip, retries,
request IDs, application metrics, configured logging and signal shutdown.
Haskell remains the default and behavioral reference. Exact tested identities,
coverage and limitations are retained in the
[comparison and approach review](../../../docs/go-migration/services/proxy/oracle/REVIEW.md).

Use pinned Go 1.26.9; the only external module is YAML v3.0.4. From the root,
run `python3 go/check.py proxy` with the pinned toolchain on PATH for formatting,
tidy/verify, vet, unit/race, executable build, vulnerability and license checks.
Run `python3 -m unittest discover -s go/services/proxy -p 'test_*.py'` for
packaging boundary checks. The copied local license is AGPL-3.0-or-later and
YAML is MIT. The license scanner's unpublished-module HEAD URL warning does
not establish a published license URL; local license files were reviewed.

Run `cmd/proxy` with `-c` or `--config-file`; the default configuration path is
`/etc/wire/proxy/conf/proxy.yaml`. Existing provider overrides, YAML option names
and secrets mounts are retained. Production TLS is always verified.
`disableTlsForTest` selects plain HTTP exactly for the existing test role; it
never disables certificate verification for HTTPS. Buffered provider requests
retain five-second response-header timeouts; raw forwarding preserves Haskell's
explicit absence of that timeout. Incoming cancellation still cancels requests.

To build a local optional image from a clean committed checkout:

```sh
python3 go/services/proxy/build_image.py --receipt /absolute/new/build-receipt.json
```

The scratch image runs as UID 65534, contains the executable and the trusted CA
bundle copied from the pinned Haskell reference. The receipt records real source,
compiler, binary and immutable local image identity; its `.ca.json` sidecar
records trust-bundle provenance. This does not push or deploy an image. Tag and
publish through your ordinary approved release process before cluster use.

Select the resulting image through existing Helm values `image.repository` and
`image.tag` (or `proxy.image.repository`/`proxy.image.tag` at the parent chart).
Only the proxy image changes; retain existing service/config/secrets/probes and
other services. Omit the override to retain Haskell. Switch back using the previous
pinned Haskell image and the same configuration. Proxy owns no database or queue.
The upstream `do-not-use` default tag is a placeholder, not a runnable release.

Application metric names, labels and counters are compared against Haskell.
Native GC diagnostic names/units are retained, with runtime-specific values:
Go CPU is averaged over collections since the previous sample; wall time is the
latest stop-the-world pause. These are not identical to GHC's per-collection CPU
and elapsed measurements. Latencies and live connections also vary by runtime.
Do not infer equivalent collector costs from shared names.

The shared runner compares actual Haskell and Go processes using verified test
TLS and synthetic providers. It reuses upstream HTTP request specifications;
it does not claim execution of the original full-backend Haskell integration
executable. Haskell compiler/unit tools are unavailable in this environment;
Haskell source, build inputs and chart defaults remain unchanged. No production
rollout or blanket parity for every possible provider/configuration input is claimed.
