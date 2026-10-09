# Patched Go rebuild review

Status: approved for this finite patch requalification; original77 and all four image smokes pass.
Reviewer: codex, same-author review under the user's continuing explicit delegation
and2026-10-09 request to rebuild earlier services now. No independent-review claim,
production switch, remote push or contract/security exception.

Review pins only: Go1.26.9 module minima for proxy/cargohold/cannon/gundeck/tooling,
x/net0.60.0 for cargohold/tooling. No service logic or unrelated dependency changes.
Exact four-service clean source c1120238f8ecd7480742156df6c5dd2d2ec41f56; exact
compiler/image/binary identities and five passing quality logs retained in
[qualification](../../docs/go-migration/qualifications/go-patched-20261009/README.md).
Existing patched federator source845956f630b791645a9c12d23918d8c9c3bc7507 and
binaryfbe1d6d62762e79da64a15a6e06af9ce7448ab2ace0ca33530ec261aa46a4595 reused.

All module security scans report no known vulnerabilities and required checks pass.
Actual images contain the exact extracted executables selected for original tests;
no equivalent-rebuild assumption. Gundeck lifecycle helper also uses Go1.26.9.
New bounded smoke coordinator reuses the existing owned stack only after original
assertions finish; no daemon-wide prune, host ports, shared service target, TLS
bypass or oracle changes. Hooks accept only the existing fixed sleep command.
Fresh77 and all four image smokes passed: no failures/errors/skips, original process0,
owned environment cleanup0; all selected executable hashes matched each suite.
Containers use defaults/nonroot/read-only/capDropALL/no-new-privileges/no host ports,
health200 and SIGTERM exit0 (0.039-0.055s). Gundeck helper sleep10 exits0/10.055s,
unsupported compound command exits2. Receipt: qualification/image-smokes.json.

Historical receipts/done tasks stay immutable. Current compatibility claims were
cleared while changed services were being rebuilt; new per-service verification
records renew exact tested pins afterward, retaining historical gates. Existing
federator receipts are reused only for unchanged inputs. No claim of rerunning
all old direct comparisons/storage switchbacks; no new speculative gate. Proxy's
existing formal native-GC contract limitation remains, not waived by this patch.
Native Haskell tools are unavailable/unrun and Haskell packages/defaults untouched.

Final results: original proxy5 + cargohold27 + gundeck40 (including cannon35) +
federator5 =77, no failures/errors/skips. Exact five hashes verified in all four
suite receipts, controller process0/cleanup0. All four actual rebuilt image smokes
pass default config path, nonroot/read-only/capDropALL/no-new-privileges/private
namespace/health200/SIGTERM0. Gundeck's existing ten-second lifecycle hook passes;
unsupported compound command is rejected. No pipeline failures or aborted patch
runs occurred. Stop finite testing here; no service logic changed.
