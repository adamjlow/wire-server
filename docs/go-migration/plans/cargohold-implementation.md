# Go cargohold implementation

Replace the entire cargohold process optionally. Haskell remains default and
supported; existing S3 object/metadata schemas and configuration remain the
cross-language boundary. No shared runtime or Haskell abstraction translation.
Source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; pinned publisher runtime and
qualified Haskell tests are retained in tools/parity/docker/baselines/. Actual
supplemental observations live in tools/parity/cargohold/reference.json.

The preceding goal turn made verified progress: 20 real reference observations
now resolve parser/storage unknowns and characterize the two authorization
findings. The user choice on inherited federation UUID matching remains pending;
contract approval/state advancement and conflicting boundary implementation must
wait. Legacy OTR reads use parseable uploader metadata but permit other users,
consistent with shared encrypted assets. Do not invent an uploader-only rule.

Use one additive module at go/services/cargohold with Go 1.26.8. Standard net/http,
crypto/rand, UUID validation/formatting, RSA CloudFront signing and small interfaces
for the actual S3/Brig/federator boundaries. Select AWS SDK Go v2 config/S3 clients,
existing YAML parser and x/net/http2 for h2c outbound federation. Check concrete
module versions, licenses and vulnerability status before committing dependencies.
No new generic service framework or global Haskell/tooling dependency. Detailed
matrix, implementation slices and actual dependency receipt follow below.

Security: ingress-authenticated principal headers remain the existing trust
boundary; strict UUID/domain/token and typed configuration parsing must reproduce
reviewed reference syntax/errors. Never weaken or secretly tighten a contract to
pass tests. Verified TLS, standard SDK signing/credential chain, bounded streaming,
context cancellation and generic reviewed errors. Only synthetic test data,
private task-owned Docker stores/peers, fixed argv and owned cleanup. No production
credentials, deployments, remote messages or push. Known unsafe reference behavior
requires the explicit disposition requested under GUARDRAILS.md.
