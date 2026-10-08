# Two Go components with Haskell peers

The user's explicit `ok go` authorizes this combined private Docker run. The
earlier explicit self-review and autonomous-development delegation applies;
reviewer is codex (self-review delegated by user), the same actor as author.
No independent reviewer or production deployment is implied.

Result: all 32 unchanged original compiled tests pass, no skips/errors: five
proxy tests followed by 27 cargohold tests in the same disposable environment.
Both Go services are selected before static peer startup and remain selected
for both suites and their dynamic service launches. Both test backend domains
use that selection. The tests and catalogue are unchanged.

Both [proxy](combined-proxy.run.json) and
[cargohold](combined-cargohold.run.json) receipts record Go proxy and Go cargohold
with the expected binary SHA256. All nine other staged backend executable hashes
match the Haskell executables in the new runner's preflight receipt. Nginx,
integration executable and schema/setup utilities remain the publisher tools.
The original XML, candidate revisions, fixture and dependency hashes,
runner build/input identity and [environment](combined-environment.json) are
retained. The runner/controller revision is
`a998e043ad4ac5e382ba3609f4c8a332e4fdd8be`.

Go proxy source: `0fae335b5cb5df7b7794bde68aea8177e327d3ef`, SHA256
`7868aa4b6b914daeb65a8033506dc03963af4ba34b37f0ef0bf4e2b2e7001238`.
Go cargohold source: `68a36b9137ce26566b2db91902469e8159885656`, SHA256
`d3591c8f7cbbb5ecbbf9c1425feb29c2b1867e270156337a0cf639eee43f7467`.
These are the previously validated binaries; no service source changes or
supplemental repeats were needed. Proxy production source has not changed since
that source pin. Haskell reference provenance remains the immutable trusted
publisher image, not independent binary/source attestation.

The controller passes each executable as a read-only mount on an internal Docker
network with synthetic data, no published ports and bounded resources. It rejects
missing/duplicate/unsupported selections, changed binaries and receipt mismatches.
Both XML reports use the existing required-case/no-skip validator. The host checks
both selected hashes in both suite receipts. Test and owned cleanup exits are
zero; no unrelated stack was touched. Twelve runner/controller tests and all 32
migration validator tests pass; diff/scope checks pass.

Self-review approves the bounded combined configuration evidence. This does not
claim the entire backend e2e suite ran. No default, security contract, production
selection or upstream service source changed. Existing Haskell build checks were
not rerun locally because no Haskell code changed; their prior CI remains applicable.
