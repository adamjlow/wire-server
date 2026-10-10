# Optional Go Spar

This is an isolated whole-service Go replacement for Spar. Haskell remains
supported and the default. The candidate implements SAML SSO, IdP and SCIM token
management, SCIM users/groups, discovery and internal cleanup through the existing
HTTP and Cassandra contracts. Native Groups PATCH remains unsupported, with its
actual native error/media/body responses preserved.

The current executable passes Go quality/security checks, actual native protocol
comparisons, durable Haskell→Go→Haskell switchback and optional container operations.
Final mixed-stack qualification and availability approval are still pending.
See the [plan](../../../docs/go-migration/plans/spar.md) and
[frozen contract](../../../docs/go-migration/services/spar/contract.md).

Use the pinned Go 1.26.9 toolchain with `GOTOOLCHAIN=local` and `GOWORK=off`.
From the repository root, run `python3 go/check.py spar`. From this module, build
`CGO_ENABLED=0 go build -trimpath -o spar ./cmd/spar`. The CLI accepts
`-c`/`--config-file`; its default is `/etc/wire/spar/conf/spar.yaml`. Startup checks
existing Cassandra schema23 before listening and never creates or rewrites schemas.
Single/multi-ingress, configured peers, verified Cassandra CA, TTLs and stored
legacy/v2 identities follow the frozen native contract.

`build_image.py --candidate <binary> --candidate-receipt <receipt> --receipt <new-receipt>`
packages a clean, static, qualified executable with trusted CA roots, linked licenses
and corresponding source. The scratch image runs as65534:65534 with the native
config path. The optional [Helm post-renderer](../../../tools/parity/spar_helm_postrender.py)
accepts an explicit image argument; omitted selection preserves the rendered default
bytes. Selection changes only Spar's image and retains RollingUpdate/config/probes.
Qualification builds local images; it does not publish or deploy them.

The owned Docker harness runs unchanged compiled Haskell integration assertions;
no native toolchain/dependency installation or manual testing is required. Current
evidence is under `qualification/evidence`, including retained failures. Exact
artifact/source pins and finite test scope belong to each receipt: an earlier
101-test construction pass does not qualify a later executable. Final intended
profile is `proxy-cargohold-cannon-gundeck-federator-background-worker-spar` (225 tests).
Native unit binaries and a full Haskell rebuild are unavailable in this environment;
no native build or unit-test pass is claimed. No Haskell source was changed.

Dependencies use maintained gocql2, YAML, x/text, goxmldsig canonicalizers, etree and
standalone Decred RIPEMD160, without patched forks. The pinned-key signature adapter
preserves native validation and legacy algorithms. Spar's service-local RSA
compatibility default retains the native small-key policy without bypassing signatures.
See [security notes](SECURITY-NOTES.md): inherited weaknesses require separate
policy work. The finite comparisons do not claim exhaustive malformed-input error
text, production load capacity or an independent security audit.
