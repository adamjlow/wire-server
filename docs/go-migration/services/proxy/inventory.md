# Proxy inventory

Inspected reference: `f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
Task: `proxy-inventory`; author: Codex. This records source observations, not
passing Haskell-versus-Go evidence. The reference files were not modified.

## Boundary and entry points

`services/proxy/exec/Main.hs` runs `Proxy.Run.run` with YAML config, defaulting to
`/etc/wire/proxy/conf/proxy.yaml`; `--config-file`/`-c` selects another path through
`libs/types-common/src/Util/Options.hs:getOptions`.

The seven provider handlers are registered in
`libs/wire-api/src/Wire/API/Routes/Public/Proxy.hs:ProxyAPI` and implemented in
`services/proxy/src/Proxy/API/Public.hs:servantSitemap`:

| Inbound path under `/proxy` | Behaviour observed in source |
| --- | --- |
| `/giphy/v1/gifs/...` | GET; retain `/v1/gifs/...`; prepend configured `api_key` |
| `/youtube/v3/...` | GET; retain `/youtube/v3/...`; prepend configured `key` |
| `/googlemaps/api/staticmap` | GET; upstream `/maps/api/staticmap`; configured `key` |
| `/googlemaps/maps/api/geocode/...` | GET; upstream substring begins `/maps/api/geocode`; configured `key` |
| `/spotify/api/token` | Forward body to upstream POST `/api/token` with configured Authorization; source handler imposes no inbound method check |
| `/soundcloud/resolve?url=...` | Required URL query; upstream GET `/resolve` with configured `client_id` |
| `/soundcloud/stream?url=...` | Required URL query; require configured upstream host and HTTPS; require 302 plus Location; return empty 302 |

Static map, Spotify and both Soundcloud routes use `Endpath`; suffix acceptance
on raw prefix routes must be characterized, not inferred from route names.
`Proxy.API.Internal` provides unversioned GET `/i/status` returning empty 200.

## Dependencies and effects

`Proxy.Env.createEnv` owns an HTTP connection manager, logger, reloading secrets
config and reload thread. The inspected source has no database, S3 or queue
client/state. `cassandra-util` supplies endpoint option types; its presence in
Cabal dependencies does not imply database access.

Providers are Giphy, YouTube/Google Maps, Spotify and Soundcloud. Their host/port
can be overridden in config. `proxy` filters case-insensitive `access_token`
query keys while preserving other query pairs and prepending its configured
secret. `basicHeaders` allows only Accept and Content-Type for the Spotify
request, plus configured Authorization. Provider error responses and exact
forwarded headers/redirect behaviours require runtime comparison against the
actual Haskell HTTP libraries; never replace them with assumed Go defaults.

Reverse-proxy 502 responses are retried twice with 5 ms pauses. Spotify and
Soundcloud HTTP calls retry selected transport exceptions twice with exponential
backoff from 5 ms. Haskell `http-client` redirect/error behaviour is an important
transitive constraint. Source-secret reload happens every 60 seconds.

## Security and middleware

The service injects synthetic/provider secrets server-side. Do not log real
query secrets or Authorization headers. Soundcloud stream enforces HTTPS and
an exact configured host in `soundcloudStream`; this is an SSRF-sensitive
boundary requiring negative tests. `disableTlsForTest` exists in Haskell; use
trusted local TLS fixtures by default in the new harness rather than disabling
candidate TLS verification.

`Proxy.Run.middleware` includes version parsing/disabling, request IDs,
Prometheus metrics, request gunzip and error catching. The version middleware
strips supported `vN` prefixes, rejects disabled/unknown versions, and rejects
version-prefixed `/i` and `/api-internal` paths. Shared version source currently
defines V0 through V19; confirm which are development versions before pinning
config expansion. Public auth enforcement belongs to deployment/ingress routing;
the proxy handler itself is not a bearer-token verifier. Keep the existing trust
boundary rather than inventing a second authentication protocol.

## Configuration and operations

`Proxy.Options.Opts`: bind endpoint; provider overrides; secrets file;
`httpPoolSize`; `maxConns`; log level/format/netstrings; disabled API versions;
test TLS option. `Proxy.Env` sets outgoing response timeout to five seconds,
connection count to `httpPoolSize`, idle count to three times that count.
`maxConns` is parsed but its enforcement was not established by this inspection.

`charts/proxy/` provides existing config/secrets mounts, HTTP proxy environment
variables, `/i/status` probes, service identity, resource limits and image
selection. The default chart continues selecting Haskell. `Proxy.Run.run` uses
the shared shutdown server helper and closes the reload thread/logger. Shared
metric names/labels and log formatting need operational characterization.

## Existing tests, evidence and gaps

- `integration/test/Test/Proxy.hs` has fake-provider scenarios for all provider
  families; `integration/test/API/Proxy.hs` contains request helpers. These files
  were read, not executed. Use their stimuli as inputs to an independent runner.
- `services/proxy/test/unit/Main.hs` references old module/API signatures. The
  current `proxy.cabal` has no test-suite declaration, so this file must not be
  advertised as a passing current unit suite.
- `services/proxy/test/scripts/proxy-test.sh` assumes a running backend/provider
  setup and is not a hermetic validation command.
- No complete differential runner or approved runtime evidence exists yet.
  Required gaps include timeout/retry traces, query duplicates/case/order,
  escaping, redirects, malformed requests, gzip, secret reload, TLS/SSRF,
  error body/content-type, headers, versions, metrics and shutdown limits.
- Native Nix/Cabal/GHC are unavailable here. Docker works. A pinned published
  image can provide a runtime only after its reference/source identity is
  established or separately approved as the explicit oracle baseline.

Suggested next tasks: source-derived contract review; optional toolchain;
independent characterization runner; candidate implementation; differential
verification; optional image selection with default-render/switchback checks.
