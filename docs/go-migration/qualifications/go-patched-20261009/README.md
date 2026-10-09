# Patched Go artifact qualification

Status: approved for this finite patch requalification; original77 and all four image smokes pass.
User explicitly requested this rebuild on2026-10-09; same-author review delegated.
Haskell remains supported/default. All images are local; no push or production switch.

Four earlier service modules and the checking-tools module now require Go1.26.9.
Cargohold and checking tools use x/net0.60.0; federator already used both patched
versions. Other service-specific dependency versions remain unchanged. Source:
c1120238f8ecd7480742156df6c5dd2d2ec41f56. Existing security scanner is never bypassed.

| Service | Compiler | Build source | Binary SHA256 prefix | Local image SHA256 prefix |
| --- | --- | --- | --- | --- |
| proxy | Go1.26.9 | c1120238f | abf252dbe14c | 68d057907e7c |
| cargohold | Go1.26.9 | c1120238f | 1838097eef9d | 836689713898 |
| cannon | Go1.26.9 | c1120238f | ced2d1923a1c | 619daec4c735 |
| gundeck | Go1.26.9 | c1120238f | a50512ef30d7 | 3aea8f0ea0f3 |
| federator | Go1.26.9 | 845956f63 | fbe1d6d62762 | 9cf75f4fb355 |

Full identities are in [artifacts.json](artifacts.json). Each rebuilt executable
was extracted from its actual new image, hashed and matched against builder and
compiler receipts, including clean VCS source and CGO-disabled linux/amd64 build.
Gundeck's shutdown helper was also rebuilt with Go1.26.9. Federator's existing
patched binary/image is unchanged and reused, not relabeled as a new build.

Format/tidy/module verification/vet/race/unit/build/vulnerability/license checks
pass for four changed services and checking tools; retained *-quality.log files
report no known module vulnerabilities. Unchanged federator's actual passing
[quality log](../../../../go/services/federator/evidence/final/quality.log) is reused.
License reports retain informational local-version/assembly-discovery warnings;
allowed-license checks pass, with no warning or scanner suppression.

The patch changes build/dependency pins only, not service logic, schemas, Haskell,
reference captures, shared assertions or Helm selectors. Existing whole-service
contracts, frozen captures, storage switchback and selector proofs remain
historical evidence for unchanged service inputs. Patch requalification requires
fresh original77 with exact five selected Go binaries and remaining Haskell
peers, plus actual default-config/nonroot/read-only container health/shutdown and
existing gundeck lifecycle helper checks. It does not claim all old supplementary
matrices were rerun or that every upstream integration test was executed.

[run.py](run.py) uses the existing owned environment/controller and unchanged
original compiled test assertions. Only after the original runner exits0 does it
start a separate dependency-tunnel namespace and smoke the four actual images;
this does not introduce extra service consumers while original assertions run.
All network access stays inside the owned disposable stack, with synthetic data
and no host ports. Images and namespace are removed in finally before existing
controller removes only its owned dependencies, volumes and networks.

Historical Go1.26.8 receipts remain accurate and immutable; use current artifacts
rather than interpreting old executables as patched. The record renews security
and functional evidence for this bounded patch. Proxy's pre-existing formal
native-GC diagnostic contract limitation is unchanged; no exception is approved.
Native GHC/Cabal/Nix checks are unavailable/unrun; Haskell packages and upstream
CI remain unchanged. Reclaiming regenerable pre-patch Go compilation-cache entries
freed1.8GiB for the owned run; binaries/receipts/module downloads/Docker data retained.

Final results: original proxy5 + cargohold27 + gundeck40 (including cannon35) +
federator5 =77, no failures/errors/skips. Exact five hashes verified in all four
suite receipts, controller process0/cleanup0. All four actual rebuilt image smokes
pass default config path, nonroot/read-only/capDropALL/no-new-privileges/private
namespace/health200/SIGTERM0. Gundeck's existing ten-second lifecycle hook passes;
unsupported compound command is rejected. No pipeline failures or aborted patch
runs occurred. Stop finite testing here; no service logic changed.
