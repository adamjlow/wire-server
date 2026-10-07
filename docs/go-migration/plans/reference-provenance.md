# Pinned Haskell reference discovery

Find a defensible source/image relationship for proxy and cargohold at inspected
upstream SHA f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Published proxy 5.37.4 has
immutable image digest but no source label; release tags/dates are insufficient.

Use an isolated task cache, never edit reference code in this checkout. Export
the exact git tree, initialize only the exact publicly accessible generic-message
submodule in that copy, and record archive hashes/gitlink. Do not synthesize an
enterprise stub or replace dependency pins. Try selected-service Nix derivation
evaluation and inspect the closure/expected output before deciding whether a
build fits available resources. Source/image matching and real build receipts
remain unverified until actual evidence is available.

Security/environment: Python standard library, verified HTTPS downloads, fixed
argument-vector commands, no credentials or interactive auth, no shell commands
from task data. Docker nixos/nix@sha256:c7cc6c8cb5d81bed19997247629604708fda95c99c43ac362daa05b6a68e8a24
gets a read-only source mount. It may fetch build dependencies; no Haskell service,
existing integration suite or parity environment is launched. Use synthetic data
and task-owned temporary directories/containers only. Preserve original source
and Nix/Cabal inputs exactly. Root build output filter applies to build commands.

Plan: export/source receipt; public submodule retrieval; Nix evaluation of
`packages.x86_64-linux.wireServer.imagesNoDocs.proxy.drvPath`; inspect outcome,
then repeat for cargohold if evaluation succeeds; record blockers/provenance.
Do not claim source provenance from the evaluated store path alone: it is an
input/output identity to check against a trusted build or published receipt.

Acceptance: record verifiable pins and results or explicit missing inputs/build
constraints, with reproducible commands and no invented reference verification.
Native Haskell tooling and whole-project enterprise-source access are unavailable
until independently provisioned. This discovery advances no service gate.
Rollback: remove task-owned temporary source/container files; root is unchanged.

## Observed result

Both selected service image derivations evaluated successfully with the exact
public source and generic-message-proto gitlink, without an enterprise stub.
All 6,883 tracked source blobs/executable modes match the pinned git tree.

For both published immutable images, Entrypoint, Env and User exactly match the
source-evaluated Docker base configuration. In particular, the executable output
paths match. This ties the trusted publisher's image configuration to the expected
Nix derivation output identity. It is not an independent binary rebuild, signed
CI build attestation, completed Haskell unit run or runtime parity result. Human
reference review must assess that provenance method before accepting the oracle.

The dry run succeeds, but needs 389 uncached derivations and 1,153 fetched paths
(1,570.41 MiB download; 8,222.39 MiB unpacked), before build outputs/scratch.
Approximately 15 GiB is free. No full compilation was started during this
discovery. Existing published images remain available for the approved local
parity environment; this discovery does not substitute for quality checks.

The task-owned `wire-migration-reference-builder` container is stopped and retains
its Nix evaluation cache for reuse. No service or integration suite was launched,
and root Haskell sources/defaults are untouched. Native whole-project checks and
enterprise-source access remain unprovisioned.

### Reproduce the source/output check

1. Export source with `git archive --format=tar f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e`.
2. In a separate copy, retrieve generic-message-proto at the exact gitlink below.
   Preserve original files, executable modes and Nix/Cabal inputs; add no enterprise stub.
3. Use the pinned Nix Docker image above and read-only source mount. Run
   `nix --extra-experimental-features 'nix-command flakes' eval --raw 'path:/source#packages.x86_64-linux.wireServer.imagesNoDocs.proxy.drvPath'`
   and the analogous cargohold attribute. Both evaluations exit 0.
4. Read the image derivation with `nix derivation show`, follow its `-conf.json.drv`
   then `-base.json.drv` inputs, and decode the latter's `env.text` JSON. Do not
   execute command strings taken from derivation/task metadata.
5. Pull each image by digest and compare `docker image inspect` Config Entrypoint,
   Env and User with that source-evaluated base JSON. Record actual image IDs and
   full derivation/receipt hashes; tags or store-path text alone are insufficient.
6. Optional build planning uses `nix build --dry-run --no-link` with both selected
   image attributes and the root output filter. A dry run is not compilation.

### Recorded identities and outcomes

```json
{
  "source_revision": "f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e",
  "source_archive_sha256": "c9f4815a5abefe3b8cec863ded56347b2a05834dbb95aa082999a70513051a93",
  "generic_message_gitlink": "08e002cae560b0a588feee9f7d31929f9e658820",
  "generic_archive_sha256": "ea453f39a1f192bad2c202ef20990f23f45ac103c736dd2e91f692831468a371",
  "enterprise_gitlink": "0cbae9136e4e0ea54e621ddef45b86966223a135",
  "enterprise_present": false,
  "source_blob_count_verified": 6883,
  "references": {
    "proxy": {
      "image_digest": "sha256:460629f6358c93892c3fd6ae5177c685bd787212900339a3bdab2530f875a8f4",
      "image_config_digest": "sha256:befc5a172c75f28353fbaea2d6550c8279c25f2444bb624ab39d9114d82a5235",
      "expected_config": {
        "Entrypoint": [
          "/nix/store/zh8pbs6w3dvxjdxasrqbpm7gqgck448y-dumb-init-1.2.5/bin/dumb-init",
          "--",
          "/nix/store/abg25gzxyp9ni145za827254xcd9jk28-proxy/bin/proxy"
        ],
        "Env": [
          "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt",
          "LOCALE_ARCHIVE=/nix/store/78i8xfh6bdkcnzc1bs3fpj7dbgvxxa4h-glibc-locales-2.42-61/lib/locale/locale-archive",
          "LANG=en_GB.UTF-8",
          "OTEL_SEMCONV_STABILITY_OPT_IN=http"
        ],
        "User": "65534"
      },
      "output_identity_matches": {
        "Entrypoint": true,
        "Env": true,
        "User": true
      },
      "source_base_derivation_sha256": "93799db8365b6ca34f7fc6fb52b44a86679878dbc9723752ad5e9aae5705fd35",
      "method": "source-evaluated-nix-output-identity",
      "independent_binary_rebuild": false
    },
    "cargohold": {
      "image_digest": "sha256:160a5d2403d04f1bf222732271b64ef87e1d18597e7784d99350a70ebfee3226",
      "image_config_digest": "sha256:63f7d5eb504a2228432d24b92a5856eaf12cb3a3ecf15b098557e282baa16144",
      "expected_config": {
        "Entrypoint": [
          "/nix/store/zh8pbs6w3dvxjdxasrqbpm7gqgck448y-dumb-init-1.2.5/bin/dumb-init",
          "--",
          "/nix/store/wwfbpmgsy8rr9jkv8h909hpxn3vp787z-cargohold/bin/cargohold"
        ],
        "Env": [
          "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt",
          "LOCALE_ARCHIVE=/nix/store/78i8xfh6bdkcnzc1bs3fpj7dbgvxxa4h-glibc-locales-2.42-61/lib/locale/locale-archive",
          "LANG=en_GB.UTF-8",
          "OTEL_SEMCONV_STABILITY_OPT_IN=http"
        ],
        "User": "65534"
      },
      "output_identity_matches": {
        "Entrypoint": true,
        "Env": true,
        "User": true
      },
      "source_base_derivation_sha256": "0257ff1ae569f326fd429fd8fe8d28cf34d1162542d839d79a49b5edc717082d",
      "method": "source-evaluated-nix-output-identity",
      "independent_binary_rebuild": false
    }
  },
  "unit_tests_run": false,
  "parity_tests_run": false,
  "build_dry_run": {
    "derivations_to_build": 389,
    "paths_to_fetch": 1153,
    "download_mib": 1570.41,
    "unpacked_mib": 8222.39,
    "wire_haskell_compilation_run": false
  },
  "log_sha256": {
    "proxy-eval.log": "22721e168a7ba6bf39161940be18b1ce6e898c2a38a493a89bc15634eebbfaf4",
    "cargohold-eval.log": "9b79bb8898d75b8bfa84faacc5a0bc792782bec6471ddb872d6242c434887093",
    "build-dry-run.log": "a3a93c5365da20bc079e22633ea05910774235b85a7e4414c76cb8910c931435"
  }
}
```
