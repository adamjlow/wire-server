# Restart on another server

Transfer handoff: 2026-10-12. Work on `origin/go-migration`; `develop` tracks
upstream. Haskell remains supported, default and the behavioural reference.
The user authorized committing/pushing this work for transfer. No deployment,
contract exception, security change or Haskell removal follows from that request.

## Current progress and next action

The authoritative task is [brig-implementation](../../../../.migration/tasks/brig-implementation.json),
**active**, with [Brig implementing and compatibility null](../../../../.migration/services/brig.json).
Read [root policy](../../../../AGENTS.md), [Go policy](../../../AGENTS.md),
the [Brig plan](../../../../docs/go-migration/plans/brig.md) and task handoff first.
No owned test containers/controllers were running at transfer preparation.

Latest implemented source is `00ebe6cf604856cc6ad52ff1339a1ae0abe6451f`:
provider service profile PUT, preserving native batch/index operation order.
[Full pinned Go checks/static unit evidence](evidence/provider-service-update-build/README.md)
passes, executable SHA256
`7b85b14a36a62190506e37bc03e529ab892b0fb4a3217fb4816331aef9b1d142`.
Tagged storage fixtures compile only. **Native/API/index comparisons and the
original service-update Accept test have not run for this increment.**

Latest fully compared service-creation slice is source
`839ef3030cd6c015649fa30f62956bc2562570af`, executable SHA256
`8092495c11e60074969652750187c9b667adf8e71e67ff3f556a792bec0d7011`:
[its evidence](evidence/provider-service-create/README.md) retains native2/Go24
unchanged original executions, actual CQL fingerprints, native readback/cleanup,
prior queue/provider regressions and failed attempts. It does not qualify the
newer profile-update executable or the whole service.

Resume the finite profile-update matrix already specified in the plan:

1. Extend `paired_reads.py` with an opt-in service-profile-update comparison,
   using separate native-created enabled services. Go connection configuration
   is not implemented; enable these actors through native first.
2. Reuse unchanged `Services.testUpdateServiceUpdateAcceptHeader` against native
   and selected Go. Compare default/JSON/plaintext Accept, empty/null updates,
   invalid/missing inputs, disabled metadata and enabled simultaneous name/tags.
3. Execute `TestOwnedServiceIndexes` with the tagged fixture to compare actual
   prefix/tag rows and native readback. Preserve the native operation order;
   simultaneous changes may retain old-tag rows under the new name. Record this
   inherited concern rather than repairing native behaviour.
4. Keep new cleanup jobs after existing single-delivery queue assertions.
   Commit source before building/running; use fresh external outputs and exact
   receipt/binary hashes. Retain failures, managed exits and independent cleanup.

Then continue remaining Brig contracts, including service connection/search/
whitelist/bot APIs. Whole-service acceptance still requires the task's complete
535 original executions with eight Go services and Haskell peers, full contract
families, three storage modes, durable switchback, loaded shutdown and image/
selector gates. Complete Brig before starting Galley (currently registered,
not implemented). No whole-Brig completion or compatibility claim is made.

The [migration overview](../../../../docs/go-migration/README.md) and
[service records](../../../../.migration/services) cover prior services. Optional
implementations/selectors exist for proxy, cargohold, cannon, gundeck, federator,
background-worker and Spar. Proxy's formal compatibility limitation remains;
previous batched225 evidence is historical and is not a current eight-Go pass.
Unavailable local full Haskell builds and inherited native SCIM failures stay
recorded; do not repair Haskell or repeat unrelated checks to obtain a pass.

## What Git contains and what must be transferred

Git contains implementation, plans, task state, pinned build definitions,
original-test harness, synthetic fixture source and retained receipts/logs.
Binaries, Docker image layers, Go/module/build caches and historical external
controller scripts are outside Git. Do not commit these artifacts.

On the old server a prepared bundle is at:
`/home/operator/wire-transfer-20261012/`. It contains `manifest.json`,
`SHA256SUMS`, `restart-inputs.tar.gz` and `private-images.tar`. The checksummed export completed successfully; its
[manifest](evidence/provider-service-update-build/transfer-manifest.json) records
archive hashes and input/image identities. Copy the whole
bundle privately before retiring that server. The archive contains synthetic
trusted test material, not production credentials. It exports current/previous
Brig SDK outputs, Gundeck binary/receipt, native fixture dependency manifest,
Go1.26.9 with download checksum receipt, module cache and historical controllers.
Historical controller absolute paths are preserved as records; use the portable
checked-in SDK controller for fresh builds.

The Docker archive preserves these local-only Linux/amd64 image IDs:

- Brig native crypto/ICU/C++ SDK:
  `sha256:2f9e4393295d7270538dd1c666ab1345eaec06b2c2f8c217283ba6565caee399`.
- Compiled original Haskell runner/peers (source
  `3922c9e2edc13983834cab02f693fd7c22bccf42`):
  `sha256:f487c90fd364aac9daee5a1ce6e60fdba0ab74ab0c671ab0de0198e8f05659a9`.

Registry dependency images are listed in `manifest.json`; pull their exact
repository digests. A Docker save/load alone does not reliably restore registry
RepoDigest metadata. Owned volumes/databases and build cache are deliberately
not transferred; each run provisions a disposable dataset. Old untracked Python
bytecode caches have no restart value. Historical external logs are already
represented by committed scoped evidence; this is a restart bundle, not a backup
of every old cache or all seven prior executable images.

To regenerate the bundle while those local inputs still exist (Python 3.11+):

```sh
python3 go/services/brig/qualification/export_transfer.py /new/external/bundle
```

## New-server setup

Use Linux/amd64, Git, Python 3.11+, Docker with Compose v2 and Docker permission.
Install backend/compiler dependencies in containers. Allow disk for image layers,
archives, outputs and disposable databases (at least 25 GiB free is a practical
starting allowance, not a measured maximum). Registry/scanner access is needed;
backend networks remain internal with no host ports or production targets.

```sh
git clone --branch go-migration https://github.com/adamjlow/wire-server.git
cd wire-server
make migration-check migration-test
```

Verify the private copied bundle and restore it into your new user's cache:

```sh
cd /path/to/copied/bundle
sha256sum -c SHA256SUMS
mkdir -p restored
tar --no-same-owner -xzf restart-inputs.tar.gz -C restored
mkdir -p "$HOME/.cache/wire-migration" "$HOME/go/pkg/mod"
cp -a restored/cache/. "$HOME/.cache/wire-migration/"
cp -a restored/modules/. "$HOME/go/pkg/mod/"
docker load -i private-images.tar
python3 - <<'PY'
import json, subprocess
from pathlib import Path
m = json.loads(Path('manifest.json').read_text())
assert m['result'] == 'complete'
for image in m['private_images']:
    subprocess.run(['docker', 'image', 'inspect', image], check=True, stdout=subprocess.DEVNULL)
for image in m['pull_dependencies']:
    subprocess.run(['docker', 'pull', image], check=True)
PY
mkdir -p "$HOME/.cache/wire-migration/brig-20261010/sdk-go-cache-disk-20261011"
chmod 0777 "$HOME/.cache/wire-migration/brig-20261010/sdk-go-cache-disk-20261011"
```

Use a dedicated writable build cache because the compiler runs as uid65534.
The source/toolchain/modules are mounted read-only. On a clean committed checkout,
start a fresh SDK build from the repository root:

```sh
python3 go/services/brig/qualification/sdk_controller.py \
  "$HOME/.cache/wire-migration/brig-20261010/new-server-sdk-check"
```

Optional `WIRE_MIGRATION_CACHE` and `WIRE_GOMODCACHE` override cache locations.
The controller records exact revision, SDK/native archive/toolchain/executable
hashes, full Go checks, static unit execution in the pinned runner and cleanup.
Do not edit source while this or a source-mounted backend session is live.
A missing image/module is a prerequisite failure, not a passing check.

The existing `paired_reads.py --help` lists identified binary/receipt/store fixture/
Gundeck arguments and opt-in contract flags. Its dependency fixture manifest must
remain at `OUTPUT_PARENT/native-original310-fixture-corrected/environment.json`.
Use restored Gundeck and the newly built Brig/stores receipts; source revision
must match the committed checkout. Do not relabel old receipts to a new SHA.
Extend/run the pending profile-update comparison before advertising parity.
Do not replay old absolute-path controllers blindly or invent the new flag.

Without the private bundle, rebuilding is possible using the committed
`tools/parity/docker/` runner/environment plans and Brig `native-crypto/` and
`native-search/` Dockerfiles, but exact historical image IDs are local artifacts,
not pullable registry identities. Rebuilt images/toolchains require new receipts
and affected qualification; rebuilding the Haskell runner can be expensive.
The bundle avoids that dependency rebuild on transfer.
