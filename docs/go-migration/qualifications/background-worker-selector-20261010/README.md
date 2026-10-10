Actual pinned Helm3.22.0/source-identical existing Wire chart render plus committed
selector230d73811: default output is byte-for-byte unchanged; explicit selection
changes exactly one background-worker image. Recreate, both default config mounts,
probes, environment, secrets, peers and all other source-rendered fields are retained.
Actual CLI rejects bad image/missing or multiple target deployments/non-Recreate/
missing galley mount/oversized16MiB render with exit2 and no selected output.
Default/go YAML and exact source/tool/input/output hashes are retained in result.json.
Synthetic values only; no deployment or push. This is selector evidence, not current
worker original124/operations or whole-service verification.

Read-only actual original-suite XML establishes124 executions,120 distinct named
functions, four repeated Events cases. Correcting119/five metadata changes no
assertion, test list, reference behavior or required coverage. Same-author review
is explicitly delegated by the user; no independent reviewer is claimed.

Use the existing Helm command/values, adding:
`| python3 tools/parity/background_worker_helm_postrender.py YOUR_WORKER_IMAGE`
Omit the image argument for byte-identical Haskell/default rendering. The selector
never builds, pushes or deploys the chosen image. Haskell remains supported/default.
