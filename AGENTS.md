# Agent operating rules

## Mission and boundaries

Maintain an upstream-compatible Wire server with optional Go implementations of
whole services. Haskell remains supported, the default, and the behavioural
reference even when a deployment chooses Go. Preserve existing build, install,
configuration and upgrade behaviour for users who do not opt in. Behavioural
compatibility matters more than implementation similarity: do not mechanically
translate Haskell abstractions into Go.

- `AGENTS.md` is policy; `docs/go-migration/` is persistent knowledge;
  `.migration/` is current machine-readable state; CI/tests supply evidence.
- Significant migration work needs an executable plan following
  [.agent/PLANS.md](.agent/PLANS.md). Every task needs bounded write scope,
  acceptance criteria and required evidence; follow [.agent/TASKS.md](.agent/TASKS.md).
- Read broadly, write only within the claimed task's paths. Check git status
  before work; preserve unrelated changes. Parallel writers need non-overlapping
  claims and separate worktrees. Shared files need a serialized owner.
- Implementation tasks must not weaken their oracle to pass. Contracts, golden
  fixtures, normalization rules, compatibility expectations and reference
  behaviour change only in explicit, separately reviewed `contract-change` tasks.
- Advance service state only when the documented objective gates pass. Missing,
  skipped or unavailable checks are not passes. Never invent evidence or approvals.
- Haskell and Go coexist as separate service processes. Do not split a service
  across a shared Haskell/Go runtime. Do not bundle language replacement with
  unrelated architecture, storage or protocol changes unless explicitly planned.
- Operators select implementations per service. Never infer a new default or
  permission to remove Haskell from Go availability or a deployment's rollout.
  Preserve compatible data and a tested route back to Haskell.
- Keep Go work additive and upstream edits small. Do not reorganize upstream
  code, require Go for existing Haskell workflows or fork shared contracts to
  accommodate a candidate. Follow [upstream sync](docs/go-migration/UPSTREAM.md);
  reassess compatibility on upstream changes and record exact tested revisions.
- Keep fork work on `go-migration`; task PRs target that branch in `origin`.
  Reserve `develop` for tracking upstream. Review upstream merges into
  `go-migration` on separate sync branches; never force-push shared history.
- Do not silently change externally observable behaviour. Record new behaviour,
  constraints and unresolved questions in the service dossier, not just chat.
- Humans control contract exceptions, per-deployment production switches and changes
  to these guardrails. Ordinary bounded work proceeds autonomously within policy.
  See [guardrails](docs/go-migration/GUARDRAILS.md) and
  [lifecycle](docs/go-migration/SERVICE-LIFECYCLE.md).

## Existing Haskell project

Services live in `services/`, libraries in `libs/`, and the current integration
suite in `integration/`. Haskell work uses Polysemy effects and shallow services;
legacy business logic is still being moved into libraries. This is a Haskell
convention, not a requirement to reproduce its architecture in Go. Prefer record
dot syntax over lenses for simple cases. Follow the existing
[coding conventions](docs/src/developer/developer/coding-conventions.md).

# Development Environment

The development environment is based on Nix flakes. Entrypoint is the
`flake.nix` file. More Nix related code is in `nix/`.

## Local

All tools and dependencies are provided by the `direnv` environment. I.e. if
you're on a local machine (e.g. developer laptop) and the current working
directory is inside this project directory, then all required tools and
dependencies are automatically available in the environment.

Local agents (e.g. copilot-cli) will probably want to use this way.

## Cloud / non-direnv

In cloud environments or when `direnv` is not installed, `nix develop` starts a
shell with all required tools and dependencies.

To directly execute commands in this environment, run `nix develop
--command bash -c "<COMMAND>"`.

Remote agents will probably want to use this way.

# Commands for Tasks

- Create a `cabal.project.local` with optimization turned off to get faster
  feedback (improved compile times): `make cabal.project.local`
- Align Cabal and Nix dependencies: `make regen-local-nix-derivations`
- Run formatter: `make format`
- Run linter: `make lint-all`
- Compile all Haskell packages (type check and see warnings):
  `make c | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Compile a specific Haskell package (type check and see warnings):
  `make c package=<PACKAGE> | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Compile all Haskell packages and run all unit tests:
  `make c test=1 | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
- Load all Haskell packages in GHCI for faster feedback:
  `cabal repl all --enable-tests --enable-multi-repl`
  - Reload inside GHCI (faster than starting a new session): `:reload`
- Load a single Haskell package in GHCI for faster feedback:
  `cabal repl <PACKAGE> --enable-tests`
  - Reload inside GHCI (faster than starting a new session): `:reload`
- Run tests for a single package (alternatives):
  - `make c package=<PACKAGE> test=1 | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`
  - `cabal test <TESTSUITE> | grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`

## Quality expectations

Haskell changes must type check without warnings and all changed packages' unit
tests must pass. Before finishing, check the whole project, all unit tests,
Cabal/Nix dependency alignment and formatting using the commands above. Preserve
`-Werror`. Record exact failures or unavailable tools; never claim unrun checks
passed. Dependency changes require regenerating and reviewing local Nix derivations.

Do not run integration tests (`make ci`, `make ci-fast`, `make ci-safe`,
`make devtest` without a unit-test package, integration executables or test suites).
They require specific environments. Future parity integration runs require an
explicit maintainer-authorized environment and policy change first.

Build commands must end with
`| grep -vE 'Compiling|Linking|Preprocessing|Configuring|Building'`.
Use Bash `set -o pipefail` and inspect the build exit status: grep can exit 1 if
all output is filtered, and must not mask a failed build.
For fast feedback use `cabal repl <PACKAGE> --enable-tests` or
`cabal repl all --enable-tests --enable-multi-repl`, then `:reload`.
Framework validation: `make migration-check migration-test`.

Preserve [PR guidelines](docs/src/developer/developer/pr-guidelines.md), the PR
checklist and CODEOWNERS. Add an appropriate `changelog.d/` entry; retain schema,
API routing and configuration review requirements. Do not edit generated files
by hand or bypass the Nix/Cabal workflow.

## Security requirements

For each bounded coding task, state concrete security requirements and relevant
framework/version constraints in its plan before implementation. Explain security
choices and alternatives. Refine underspecified security work into scoped tasks.

- Use synthetic data/placeholders; never commit credentials, production secrets,
  customer-identifying data or unsanitized captures. Follow [SECURITY.md](SECURITY.md).
- Treat input as untrusted: validate server-side, bound resource use, use
  parameterized queries and framework output escaping where applicable. Errors
  must avoid sensitive details while preserving the reviewed external contract.
- Keep TLS verification enabled. Do not use eval or execute untrusted task text.
- Prefer established, maintained dependencies. Check selected versions for known
  high-severity vulnerabilities; do not add dependencies without justification.
- Test security boundaries and malformed input, integrate those tests into CI,
  and explain the protections. Never weaken auth, crypto or security tests to
  obtain parity; resolve unsafe reference behaviour through reviewed contract work.
