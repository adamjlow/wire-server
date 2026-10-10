# Optional image construction

Same exact original124-qualified worker89f24e7a and publisher-checksummed
OpenSSL3.5.9 are packaged in a local scratch image with source reference CA bundle,
UID65534, fixed trusted PATH, executable modes and OpenSSL/musl/GCC-runtime/AGPL
license notices. No default deployment/image selection changed.

OpenSSL is built in a disposable pinned Alpine3.24.2 container from verified local
source and signed HTTPS compiler packages; no host dependencies. Builder objects
are tmpfs and its owned container is removed. musl1.2.6-r2/GCC15.2.0-r5 package
security evidence is in security.json. Static PIE has no INTERP or DT_NEEDED;
relocation-only dynamic metadata does not require shared libraries. Actual CLI
executes in the nonroot scratch image, and current existing TestBot tests pass in
that exact image with no skip, including DHE/pin/date/cancellation/transcript/reap.

The initial compiler attempt lacked linux-headers and is retained as failed.
A too-strict no-dynamic-section assertion was corrected to no shared dependencies,
without recompiling. First scratch prototype had non-executable copied files;
explicit COPY --chmod=0755 fixed both artifacts. Its failed probe remains retained.
The committed builder/Dockerfile inputs match the successfully executed recipe.
Raw successful/failed compiler logs and source archive remain in the external
openssl-musl-3.5.9 cache; build.py/build.json identify exact inputs and outputs.

This is construction proof. Run the committed generic packager, real image health,
trusted-CA/default mounts/SIGTERM/declared operations and actual same-state durable
switchback before whole-service qualification. No production switch or remote push.

The committed generic packager now runs successfully on cleanfc99aa141 using
the same identified worker/helper. [Its receipt](committed-image.json) produces
exactly the already-tested prototype imageIDa14d2033, with identical rootfs/runtime
config/file hashes, UID65534 and worker entrypoint. The retained [packager check](packager-check.json)
accepts both actual static artifacts and rejects the earlier dynamic Nix helper.
No compiler/fixture/test containers remain. Real health/default mounts, bounded
operations and durable switchback still require execution.
