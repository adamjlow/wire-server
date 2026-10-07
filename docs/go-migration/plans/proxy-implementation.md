# Whole-service Go proxy implementation

Objective: implement all proxy handlers and shared configuration/middleware/
operations as a separate optional Go service, retaining Haskell defaults and
upstream source. Reference f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e. Approved contract:
`docs/go-migration/services/proxy/contract.md`; independent 65-case TLS baseline
and executable capture fixture are under `services/proxy/oracle` in that dossier.
Baseline is initial characterization, never complete candidate parity.

Module: `go/services/proxy`, pinned Go 1.26.8. Initially stdlib plus the YAML
organization's security-maintained stable `go.yaml.in/yaml/v3 v3.0.4`; see
https://github.com/yaml/go-yaml#version-intentions (v4 is currently release-candidate
v4.0.0-rc.6). Verify vulnerability/license/module checks before accepting this
pin. Do not use the abandoned original fork, implicit latest, or custom YAML.
No shared Go framework, FFI, reference service edits or new network protocols.

Implementation sequence:
1. Add module/license, existing YAML/CLI option shape, configurator-style secrets
   loading/reload, cancellation and graceful shutdown. Characterize unsupported
   secret grammar against Haskell before claiming complete configuration parity.
2. Implement all seven provider handlers and health: byte-preserving escaped
   paths/ordered queries, case-insensitive raw GET, Spotify POST/header allowance,
   Soundcloud URL/host/scheme/302 boundary, source errors and duplicate headers.
   Preserve five-second outgoing timeout, connection/proxy settings and selected
   retries/backoff. No extra upstream calls or credential disclosure.
3. Implement V0..V19/disabled/development routing, exact method/Endpath errors,
   shared gunzip/request IDs, metric/log schemas and termination. Characterize
   the remaining middleware/transport details independently; do not invent defaults.
4. Meaningful candidate unit/race/security tests: provider observation assertions,
   query escaping/duplicates, TLS/SSRF, outgoing credentials, retries, secret
   reload errors, gzip/version/header boundaries, cancellation and shutdown.
5. A separately scoped contract/runner task extends independent reference capture
   to the same Go image/config/stimuli. Record all response/application headers,
   bodies and ordered provider effects; only already reviewed transport variation
   may be normalized after validation. Corruption tests must demonstrate failures.
6. Complete missing reference characterization and full differential/operational
   matrix, scanner/license checks, unchanged Haskell/default chart evidence and
   optional image selection/switchback before verified/available claims.

Security requirements: trusted TLS, exact stream-host check, no eval/shell task
execution, synthetic artifacts only, no credential logs; validate config and
HTTP parsing inputs consistently with source, bound timeouts and streaming use,
close/cancel requests. Do not silently add a body-size/authentication restriction
or weaken validation just to pass. Candidate code and its unit tests cannot edit
oracle fixtures, normalization or reference code. Cargohold source/security
exceptions are not resolved by this task.

Checks: `python3 go/check.py proxy` with pinned Go on PATH; candidate gofmt,
module tidy/verify, vet, race tests, build, govulncheck and go-licenses are mandatory.
All direct build/test shell commands retain root's output filter/pipe status.
Run `make migration-check migration-test` and exact --base/--task diff validation.
Execute only the explicitly approved isolated runner, never existing integration
suites or live providers. Missing candidate cases or unrun quality checks remain
recorded as missing, not passes. Actual code/evidence commits are separate.

Rollback: remove/revert only additive candidate files and explicitly selected
image; Haskell default configs/image/chart/source remain intact. Service records
advance one stage at a time. This plan is an implementation gate, not candidate
availability or operator permission to switch a deployment.

Plan acceptance: source contract/baseline, toolchain/dependency approach, complete
bounded candidate task, independent verification sequence and rollback are
reviewed under the user's explicit self-review delegation. Unknowns are listed
and do not authorize partial verified/available claims.
