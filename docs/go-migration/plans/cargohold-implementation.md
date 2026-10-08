# Go cargohold implementation

Replace the entire cargohold process optionally. Haskell remains default and
supported; existing S3 object/metadata schemas and configuration remain the
cross-language boundary. No shared runtime or Haskell abstraction translation.
Source f6636ed53315fa7b0fc4c8aa39c0bb30c98b6d5e; pinned publisher runtime and
qualified Haskell tests are retained in tools/parity/docker/baselines/. Actual
supplemental observations live in tools/parity/cargohold/reference.json.

Twenty real reference observations resolve parser/storage unknowns and
characterize the two authorization findings. The user choice on inherited federation UUID matching remains pending;
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

## Ordered implementation and evidence

1. Record the user's federation disposition in the existing contract work, review
   the completed contract and supplemental expectations separately from candidate
   code, then advance the contracted/implementing gates through coordination.
   Preserve exact observations; changing authorization requires an explicit
   contract-change task for both references. This plan cannot approve that choice.
2. Claim one implementation task containing the module, CLI/configuration,
   listener, health endpoints, S3 adapter and a runnable bot upload/read/delete
   slice. Compare that slice with the existing reference immediately. Use existing
   proxy build/check patterns without extracting a general framework.
3. Expand to all public/versioned routes, principal precedence, user eligibility,
   tokens, retention, legacy/plain/OTR, audit metadata and multipart parser. Preserve
   S3 keys and raw metadata; Haskell and Go share the same bucket representation.
4. Add federation availability/streaming, outbound h2c requests, CloudFront and
   configured endpoint/addressing variants, cancellation/retry/error handling.
   Consume only the approved federation rule from step 1.
5. Package an optional image/chart selection, run the original 27 tests on Go
   with Haskell peers, the bounded supplements below, bidirectional storage
   switchback and one operational smoke. Review real reports before availability.
   Missing evidence stays missing; neither image creation nor unit tests establish
   whole-service equivalence. Haskell remains the omitted-selection default.

Each slice builds a runnable executable. Routine unit/race/vet/format/dependency
checks belong to implementation, rather than separate discovery tasks. Shared
oracle assertions are owned by contract work, never changed to accommodate Go.

## Finite acceptance matrix

The original test catalogue is tools/parity/e2e-tests.json. Reuse all assertions
inside its 27 cargohold functions: API (10), V3 (2), federation (8), user upload
eligibility/limits (5) and download/multi-ingress (2). Retained Haskell XML is
tools/parity/docker/baselines/haskell-cargohold.xml. Supplemental reference-only
observations are tools/parity/cargohold/reference.json (20), with exact identities
in run.json. Those observations are not candidate passes. Before implementation,
the contract owner makes the following finite supplemental assertions executable;
reuse already captured expectations where valid. Do not multiply every row by
all configurations, routes or runtimes.

| Group | Bounded cases and observable acceptance | Evidence/source |
| --- | --- | --- |
| Original suite | Exactly 27 original functions, no skips/errors; Go replaces only cargohold, all peers Haskell | Original catalogue and qualified private Docker controller |
| Authorization | Replay 10 federation get/stream positive/negative controls under the approved disposition, plus 4 legacy OTR metadata/requester controls | Existing 20-case capture; API/Federation.hs, API/Legacy.hs |
| Multipart | Replay the 6 captured uploads; add 6 source-defined cases: missing first length, duplicate length, malformed length, malformed settings JSON, missing payload header terminator, missing payload length. Assert status/error, stored bytes/metadata and open multipart uploads | API/Util.hs, API/V3.hs, S3.hs; existing captures |
| Public routing | One bot, one provider, and one user precedence request with conflicting secondary principal headers; one plain legacy lookup; one disabled-version rejection; one malformed UUID/token rejection each | API/Public.hs, API/V3.hs, API/Legacy.hs and wire-api routes |
| Contract gaps | Token header-over-query precedence, non-owner mutation denial, malformed settings order, missing metadata part, duplicate Content-Type, gzip upload, disconnect mid-stream, arbitrary chunk boundaries (8); conv/plain legacy and missing legacy object (2); internal signed URL (1); outgoing remote unavailable/transport failure/delete rejection (3); raw and URL-encoded seeded audit metadata reads (2). Assert the existing contract response and provider effects | Existing contract matrix; API/Util.hs, API/Legacy.hs, Federation.hs, S3.hs |
| Persisted representation | One upload for each of 5 supported retentions; exact key/owner/token/content-type metadata and byte equality. Private token create/renew/delete once each; assert previous token rejection. Audit enabled/disabled upload and malformed required audit metadata (3 cases) | S3.hs, API/AuditLog.hs, asset schema |
| Switchback | Haskell writes then Go reads/token-changes/deletes, and Go writes then Haskell does the same, for user, bot and provider (6 scenarios); include private token and audit metadata in these objects | Shared private S3 bucket; real binaries and object inspection |
| Signed download | Path, auto, virtual addressing (3); download-endpoint override (1); mapped/unmapped/missing Z-Host (3); Scality compatibility (1). Verify signature and actual bytes against compatible owned S3 endpoint, not URL appearance alone | Options.hs, AWS.hs, S3.hs; original tests reused where covered |
| CloudFront | One fixed-clock synthetic RSA-key URL with independent signature/policy verification; one invalid configured key startup failure | CloudFront.hs; preserve RSA-SHA1 and CloudFront base64 alphabet as existing wire protocol |
| Failures/streaming | Owned S3 injected missing object, denied HEAD, upload-part failure, complete failure, transient throttling (5); request cancellation and a multi-part 24 MiB upload (2). Compare external errors, persisted object/abort effects, retries and bounded memory | S3.hs, AWS.hs, API/Error.hs; same injection for both binaries |
| Operations/selection | One smoke per binary; omitted selection Haskell and selected Go (2 rendered manifests), CLI help/config startup, health/readiness, metrics/log shape and shutdown/drain | Run.hs, App.hs, Metrics.hs; existing chart default |

If a planned expectation cannot be derived confidently from source, capture just
that reference case before candidate consumption. Any newly found security issue
uses the same explicit guardrail; do not resolve it by normalization. Incidental
GC metrics, timing identity and random IDs/tokens are not equality targets. Compare
random values by shape/relationships and persisted semantics; freeze explicit
normalization in the reviewed oracle. Avoid matching secrets/signature values in
logs. Source-derived matrix is planned coverage, not execution evidence.

Operational smoke: 40 requests per implementation, concurrency 2, including a
24 MiB upload under the fixture's configured limits. Maximum Go RSS 192 MiB;
p95 latency <= 2 times the measured Haskell p95 + 100 ms. Include SIGTERM during
one active download; no new requests after readiness drops, active request drains
within 30 seconds. Abort after 3 consecutive unexpected 5xx or the controller's
fixed run timeout; inspect the failure rather than expanding the load. Adjusting
these acceptance thresholds requires an explicit reason before rerunning, never
a post-hoc pass. Retain raw samples and both runtime identities.

## Dependency proof (2026-10-08)

Isolated workspace outside the checkout:
`/home/operator/.cache/wire-migration/cargohold-dependencies-20261008`.
It contains only a scratch module and blank imports for config/S3/YAML/http2;
this is compilation/dependency evidence, not a candidate service. Go 1.26.8.
Pinned direct imports: AWS config v1.33.7, S3 v1.114.1, YAML v3.0.4,
x/net v0.59.0. AWS root v1.47.1, smithy v1.28.1; transitive versions are frozen
in that workspace's go.mod/go.sum and must be copied by the implementation task,
not re-resolved with latest. go.mod SHA256
`39276c42d19fea0f1d628447e85173e6f6f05413b5c2a351b7b89c93d10c7af3`;
go.sum SHA256
`d7bf353404bd81a0d13d1ded375898ce5a6133d96ffa82126cf128c916144e5a`.

Actual checks: `go mod tidy`, `go mod verify` (all modules verified),
`go build -o dependency-check .` (exit 0), pinned tooling's govulncheck 1.8.0
`-scan=module` (exit 0, No vulnerabilities found), and go-licenses v2.0.1
`check --include_tests --allowed_licenses=AGPL-3.0,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC,MIT ./...`
(exit 0). Initial build lacked transitive x/text sums; tidy resolved them before
successful checks. Scanner PATH must include the cached Go executable. Module-only
scan takes no ./... argument. Scan success reflects the database at execution,
not a guarantee about future vulnerabilities; candidate changes require new scans.

Use established SDK signing/credential handling rather than implementing SigV4.
[AWS checksum documentation](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/s3-checksums.html)
and [SDK config](https://github.com/aws/aws-sdk-go-v2/blob/main/aws/config.go)
show new automatic checksum behavior: explicitly select WhenRequired and compare
actual signed requests against the reference/Scality path. Preserve TLS verification.
Use low-level S3 multipart calls with bounded buffering instead of a generic
unbounded uploader; determine part size from original uploader semantics before
coding. Preserve the observed short-body completed partial-object behavior unless
separately changed. SDK credential precedence and retries need comparison with
AWS.hs, not an assumption that defaults match. Outbound federation uses x/net h2c;
CloudFront uses standard crypto with the reference SHA1 wire signature. YAML parses
the existing typed schema and required fields without inventing new strictness.

## Commands, retention and stopping

From a clean committed checkout, reuse the qualified immutable runner receipt
`tools/parity/docker/environment-build-receipt.json`; do not rebuild the runner
unless its relevant inputs change. Build the candidate with a full source revision
and binary SHA256 receipt using the existing proxy build pattern. Then:

```sh
python3 tools/parity/docker/environment.py prepare --receipt tools/parity/docker/environment-build-receipt.json --output /absolute/new-cargo-run --candidate /absolute/cargohold --candidate-receipt /absolute/cargo-build.json
python3 tools/parity/docker/environment.py run --output /absolute/new-cargo-run --service cargohold
```

Controller checks receipt identity; recreate a receipt if validation reports
changed inputs, never edit receipt hashes manually. Supplemental controller work
belongs in separately claimed protected oracle scopes. Use task-owned internal
Docker networks, synthetic credentials, read-only inputs and owned cleanup; no
host backend dependency installations or shared services. Record XML, request/S3
effects, images/config/fixture identities, candidate hash and cleanup result in
verification evidence. For the candidate module run gofmt, go vet, unit/race tests,
module verification and the existing go/check.py dependency/license checks after
registering the module through its scoped task. Preserve root Haskell targets.

Run `python3 tools/migration/check.py --base FULL_CLAIM_SHA --task TASK_ID` and
`make migration-check migration-test`; use existing parity tool unit checks when
changing that tool. This planning task has no Haskell source or dependency changes;
full Haskell CI/typecheck remains unrun locally (Nix/GHC/Cabal unavailable), not a
pass. Existing upstream CI remains required for source/default workflow changes.

Stop expanding validation once all matrix rows pass with retained exact identities,
clean owned teardown and review. New tests then require a concrete failure or
uncovered risk. No production cutover or push is implied. Rollback for development
is select Haskell again against the compatible bucket; prove switchback, do not
merely document it. Availability review must audit the whole matrix, never infer
completion from the original 27 alone.

## Retained scratch dependency manifests

These exact manifests record the resolved graph without creating a candidate module.

```go.mod
module wire-cargohold-dependency-check

go 1.26.8

require (
	github.com/aws/aws-sdk-go-v2/config v1.33.7
	github.com/aws/aws-sdk-go-v2/service/s3 v1.114.1
	go.yaml.in/yaml/v3 v3.0.4
	golang.org/x/net v0.59.0
)

require (
	github.com/aws/aws-sdk-go-v2 v1.47.1 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.20.7 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.5 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.10.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.38.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.51.2 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	golang.org/x/text v0.42.0 // indirect
)
```

```go.sum
github.com/aws/aws-sdk-go-v2 v1.47.1 h1:uOIZnp4PK3ZhKI0dNrJrhTEsLxbpXHTAJlwoS1pvAtw=
github.com/aws/aws-sdk-go-v2 v1.47.1/go.mod h1:bttEH6JqnUL8LepvDVfdrds/fZ5bCIxzpe3abyUrhDU=
github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 h1:GPRlPwz40I2B2VrBEASOA3Bi77NyeqejNLkifosX0rs=
github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20/go.mod h1:g7PNzKcsOKWb4fkSRBA7BZVAS6Y8IcxzN+nRohhQ1Q8=
github.com/aws/aws-sdk-go-v2/config v1.33.7 h1:vgG8uuhm7vNsxr6BsXNG+Ciesh9iLdNPzarAAu/dB6A=
github.com/aws/aws-sdk-go-v2/config v1.33.7/go.mod h1:5KWNY38altxnd5cxcNoOTrFtJ7asFEiCiWfb2m+M2pE=
github.com/aws/aws-sdk-go-v2/credentials v1.20.7 h1:mit8rRVnmt+wvT4U6putiWSlwe3eKD2n6ElgyfcP/Mw=
github.com/aws/aws-sdk-go-v2/credentials v1.20.7/go.mod h1:/2pPi2qhPug21z6l3M0+AKUqAT1zn2MhRCtnQv8R5Zg=
github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1 h1:8gALAAmacnIXh+z6VkdDanv4/IkG5APdg4DZLDTmLog=
github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.20.1/go.mod h1:Z7IJhJU+poOdJjUR2wpyY21ossQ1XS/R3Lk9Msq5kM4=
github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4 h1:CLq4+8UHCI+ZZYl/EuJxXovaIVN2xeeT8JV+dsApQ5E=
github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.4/go.mod h1:Wv4q5sAM04xAMkoOedxLx2inVf6K5FdxYp+A61L+q/0=
github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4 h1:dD4MR81I7YkpEBRk6UP9rocC2QnT3qVuXwzlYTtfGEs=
github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.4/go.mod h1:EcXV1kAFd5XwSkDHlj94gnF3q5CkJyYiIJfH8N0VmrE=
github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4 h1:7Wo47d/xn/7KttCSBd8EGYeZ7ULRFRkUHr6vkZPBzVQ=
github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.4/go.mod h1:tDB2IVC1xC3vX8o+6uRlzhTxP3g1b77CZXFX/oD2FnQ=
github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 h1:bAdDl/HkGCcGPoe25ToSHEw23VIxt6CT5fLcg111BKg=
github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19/go.mod h1:KaUzbLxv4CeSxh6ZCl9B4m7CuFenS8kUEaDs+f/DQr4=
github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.5 h1:/TYsZXdA8UTa+WCtCYSAJIr1vwl0+eho6TUgJGwFFO8=
github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.5/go.mod h1:qPqp1Uwd/BqdhPufv6oem9j5J7HNsgc2V22dUiDPn+s=
github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4 h1:29SvnfGhXjTl8ONxFwbj2rs6lbhiFXD2CgFQmbT/bXY=
github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.4/go.mod h1:wm04I5DMuNVvZHFe/dHnUxincvNbbK7AiNBbYsQivek=
github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.4 h1:pPiWfgeNxqluKEph7hvU88kuGKBPOWzO+Dk9t2zqqNs=
github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.4/go.mod h1:YlwGoIUDG/3kBQbdNOVs/xKZ9J01G8e/6D1mRBj9uTk=
github.com/aws/aws-sdk-go-v2/service/s3 v1.114.1 h1:9T6eO8/WXhij4PQaa2WaYsdekSCmT9Hw66Pw0S8do0Q=
github.com/aws/aws-sdk-go-v2/service/s3 v1.114.1/go.mod h1:9APRWGLFITKD+xzWSIyT9V7QV4bNlEuIieWlzXgGFlI=
github.com/aws/aws-sdk-go-v2/service/signin v1.10.2 h1:vrPP4O9VABnquYrDHfU+Fu0Fp6TuzaqPUu9Fh7VBBVY=
github.com/aws/aws-sdk-go-v2/service/signin v1.10.2/go.mod h1:xpo/geVldu8payT375WekctUzopG/hBU7miiqItMUlw=
github.com/aws/aws-sdk-go-v2/service/sso v1.38.2 h1:I8F/K5ahKJ4npqeGBBmMPftQy3/o1A55VRq2eUcP9Ts=
github.com/aws/aws-sdk-go-v2/service/sso v1.38.2/go.mod h1:rRD/dnm7q0HYE/I5TMaPgkWyyUGLcwuxHLABsLnQ3e0=
github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.2 h1:i5nn7Ri5CgFfNRzwlUiZOIIUA+5Vgg+unNEC0PzF1+s=
github.com/aws/aws-sdk-go-v2/service/ssooidc v1.43.2/go.mod h1:skwM/xsbR/1ReUTesv9BhpJp1VjajR7DWQnuVLwiXsQ=
github.com/aws/aws-sdk-go-v2/service/sts v1.51.2 h1:AWSPqynwEJV8J2pl/p7aizCf7a23/NGHR+rK/KLOxi4=
github.com/aws/aws-sdk-go-v2/service/sts v1.51.2/go.mod h1:26zA0GhDrLo+yiLI2yXWxqB1PdsShfLikoI7GOEgugM=
github.com/aws/smithy-go v1.28.1 h1:R/nXH00c8qcfCzQVELtRw+eLQWtzv+VAIEFJ1/xxXlQ=
github.com/aws/smithy-go v1.28.1/go.mod h1:YE2RhdIuDbA5E5bTdciG9KrW3+TiEONeUWCqxX9i1Fc=
go.yaml.in/yaml/v3 v3.0.4 h1:tfq32ie2Jv2UxXFdLJdh3jXuOzWiL1fo0bu/FbuKpbc=
go.yaml.in/yaml/v3 v3.0.4/go.mod h1:DhzuOOF2ATzADvBadXxruRBLzYTpT36CKvDb3+aBEFg=
golang.org/x/net v0.59.0 h1:5zfYln+w5XCxwrnMMJPufRgNoXEaGxl0wo5GqPXyues=
golang.org/x/net v0.59.0/go.mod h1:2DA/G1UfVbCpQPeWTmMPGY7Cs2PkBkwu743bVX5PIVg=
golang.org/x/text v0.42.0 h1:JbOZXgfeCPU9gacVtYliJqOhD+zhrEqK4LfdpmlUZqI=
golang.org/x/text v0.42.0/go.mod h1:ojzP1Z+2QtioaF8DTtO8K5q7JWVVYwZKenzujK0Zd0E=
gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405 h1:yhCVgyC4o1eVCa2tZl7eS0r+SDo693bJlVdllGtEeKM=
gopkg.in/check.v1 v0.0.0-20161208181325-20d25e280405/go.mod h1:Co6ibVJAznAaIkqp8huTwlJQCZ016jof/cbN4VW5Yz0=
```
