# Patched Go rebuild and requalification

User 2026-10-09 explicitly requests rebuilding earlier services now so all five
use the same patched Go version and migration work is clean. Continuing delegated
same-author review applies; no remote push, production switch or oracle exception.
Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e stays unchanged. Haskell default.

Bounded change: proxy/cargohold/cannon/gundeck module Go minimum1.26.9; cargohold
and checking-tools x/net0.60.0. Federator already uses patched compiler/dependency
and keeps its exact qualified binary. No unrelated dependency upgrades or service
logic changes. Keep completed historical task/evidence records immutable. Clear
three current compatibility claims while rebuilding; new verification tasks renew
exact source/binary claims after success. Proxy retains its existing formal stage
and documented diagnostic-contract limitations; this patch does not waive them.

Security: official SHA-verified compiler already pinned, GOTOOLCHAIN=local/GOWORK=off,
no TLS bypass/scanner suppression. Existing malformed-input/TLS/security regression
tests remain. Module-level vulnerability/license checks required for all changed
modules and tooling. Do not relabel historical Go1.26.8 binaries as patched.

Finite execution:
1. Commit minimal module pins, tidy legitimate sums, build from clean source.
2. Run go/check.py for four affected services and tools (format/tidy/verify/vet/
   race/unit/build/vulnerability/licenses). Reuse final unchanged federator checks.
3. Build four local images with existing builders; extract and hash actual image
   executables, retain compiler receipts, test actual container health/SIGTERM and
   existing lifecycle helper where present. Reuse unchanged selectors/Helm proof.
4. Existing Docker controller executes unchanged original77 with five exact patched
   selections and remaining Haskell peers. Same owned internal networks/synthetic
   fixtures/no host ports authorized previously; remove only owned stack resources.
   No duplicated full baselines or speculative new gates for a compiler patch.
5. Review exact artifacts and finite results, add current patch qualification links,
   new verification tasks and restore compatible claims through scoped publication.

Commands: pinned compiler PATH; python3 go/check.py SERVICE with required build
output filter/actual exit status; existing build_image.py --receipt;
 tools/parity/docker/environment.py prepare --select SERVICE BINARY RECEIPT then
run --service proxy-cargohold-cannon-gundeck-federator; make migration-check migration-test.
Evidence: docs/go-migration/qualifications/go-patched-20261009/ and
.migration/reviews/go-patched-rebuild-20261009.md. Receipt source identifies actual
clean service build; metadata completion does not trigger service rebuild.
Native Haskell checks unavailable/unrun; unchanged upstream CI retained.
