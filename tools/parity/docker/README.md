# Containerized upstream e2e runner

The runner keeps the compiled Haskell suite, Wire service executables, native
libraries and Python runtime inside Docker. Nothing installs GHC, Cabal, Nix,
Cassandra, RabbitMQ or S3 tooling on the host. The base is Wire's published
integration image, pinned by immutable digest (release 5.37.5); this is trusted
publisher provenance, not an independently rebuilt/source-attested binary.

From a clean committed checkout:

```sh
python3 tools/parity/docker/control.py build --output /absolute/new-build.json
python3 tools/parity/docker/control.py preflight --receipt /absolute/new-build.json --output /absolute/new-preflight.json
```

The first pull downloads the published runtime bundle (about 3.9 GB unpacked).
Subsequent builds reuse it and add only the small runner scripts/catalogue. The
builder uses a temporary Docker context and `--network=none` for build commands;
no repository-sized image copy or local Haskell compilation is needed.

The controller records the local immutable runner image, source revision and
input hashes. Its preflight creates an owned container with networking disabled,
read-only rootfs, non-root UID 65534, all capabilities dropped, no-new-privileges,
bounded CPU/memory/PIDs and a small writable tmpfs. Cleanup uses only the actual
created container ID. It checks the compiled integration/service binaries and
lists five original proxy and 27 core cargohold test functions. All assertions
inside those original functions are retained; tests are not translated into Go.

`preflight-pass` explicitly means `e2e_executed: false` and
`environment_ready: false`. It proves the published executables/test catalogue
are usable in Docker, not that backend dependencies or e2e assertions passed.

## Running the original suite in a provisioned environment

The image also provides `suite --service proxy|cargohold --config FILE --output FILE`,
with optional `--candidate FILE`. Configuration must be outside a `services/`
directory so the published suite uses its existing PATH service-launch mode.
Supply synthetic service configs at `/etc/wire/<service>/conf/<service>.yaml`,
original fixture material, a writable report volume, and task-owned disposable
backend dependencies/static Haskell peers. The publisher image has no standalone
`run-services` executable; static peers belong to the environment orchestrator.

For a selected Go binary, mount it read-only and pass `--candidate`; the runner
creates a temporary PATH entry for exactly that service. Other services resolve
to the pinned Haskell executables. It calls the original compiled test suite with
the catalogued tests, then requires complete successful XML results using the
shared harness checker. Missing/skipped/failed cases or nonzero process exits
cannot pass. Execution and report validation are recorded in a `.run.json` sidecar.

Full execution requires `WIRE_E2E_DISPOSABLE=1`, which is an explicit readiness
signal, **not proof of isolation or permission to use shared infrastructure**.
The orchestrator must establish a private internal Docker network, no published
host ports, synthetic fixtures/configuration and task-owned dependency volumes.
Never point these tests at production or run the upstream development Compose
file unchanged: it contains fixed container names, subnets and host port bindings.
The dependency environment and static-peer initialization have not yet been
provisioned/qualified by this runner build. No full e2e success is claimed.

Architecture: one test-runner image plus separate disposable database, object-store,
queue, cache/search and Haskell-peer containers. Keep long-lived host services and
compiler installation out of this setup. Once the private stack is qualified,
run the same original suite first with Haskell and then with one Go substitution.
